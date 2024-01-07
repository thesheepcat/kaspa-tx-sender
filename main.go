package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/kaspanet/kaspad/app/appmessage"
	"github.com/kaspanet/kaspad/domain/consensus/model/externalapi"
	"github.com/kaspanet/kaspad/domain/consensus/utils/consensushashing"
	"github.com/kaspanet/kaspad/domain/consensus/utils/constants"
	"github.com/kaspanet/kaspad/domain/consensus/utils/subnetworks"
	"github.com/kaspanet/kaspad/domain/consensus/utils/transactionid"
	"github.com/kaspanet/kaspad/domain/consensus/utils/txscript"
	utxopkg "github.com/kaspanet/kaspad/domain/consensus/utils/utxo"
	"github.com/kaspanet/kaspad/domain/dagconfig"
	"github.com/kaspanet/kaspad/infrastructure/network/rpcclient"
	"github.com/kaspanet/kaspad/util/profiling"

	"encoding/hex"

	"github.com/kaspanet/go-secp256k1"
	"github.com/kaspanet/kaspad/infrastructure/os/signal"
	"github.com/kaspanet/kaspad/util"
	"github.com/kaspanet/kaspad/util/panics"

	"github.com/pkg/errors"
)

var shutdown int32 = 0

func main() {

	prefix := dagconfig.TestnetParams.Prefix

	// Insert here the result of genkeypair operation
	myPrivateKey := "5352da6a9b87829610211727c253c34eed7f8d39ef4530db6b79e40e844bccd0"
	myAddressString := "kaspatest:qzj7mhr248ml9znje52egmarcqfk8t2zu6ju0hyp0zc3g3ate2hrj4pr0ggx3"
	recipientAddressString := "kaspatest:qzj7mhr248ml9znje52egmarcqfk8t2zu6ju0hyp0zc3g3ate2hrj4pr0ggx3"

	// Some Private / Public keys manipulation
	myAddress, err := util.DecodeAddress(myAddressString, prefix)
	if err != nil {
		panic(err)
	}

	recipientAddress, err := util.DecodeAddress(recipientAddressString, prefix)
	if err != nil {
		panic(err)
	}

	myKeyPair, myPublicKey, err := parsePrivateKeyInKeyPair(myPrivateKey)
	if err != nil {
		panic(err)
	}

	pubKeySerialized, err := myPublicKey.Serialize()
	if err != nil {
		panic(err)
	}

	pubKeyAddr, err := util.NewAddressPublicKey(pubKeySerialized[:], prefix)
	if err != nil {
		panic(err)
	}

	fmt.Println("myPrivateKey: ", myPrivateKey)
	fmt.Println("myKeyPair: ", myKeyPair)
	fmt.Println()
	fmt.Println("myPublicKey: ", myPublicKey)
	fmt.Println("pubKeySerialized: ", pubKeySerialized)
	fmt.Println()
	fmt.Println("myAddress: ", myAddress)
	fmt.Println("pubKeyAddr: ", pubKeyAddr)
	fmt.Println()

	interrupt := signal.InterruptListener()
	configError := parseConfig()
	if configError != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config: %+v", err)
		os.Exit(1)
	}
	defer backendLog.Close()

	defer panics.HandlePanic(log, "main", nil)

	if cfg.Profile != "" {
		profiling.Start(cfg.Profile, log)
	}

	// RPC connection setup
	rpcAddress, err := activeConfig().ActiveNetParams.NormalizeRPCServerAddress(activeConfig().RPCServer)
	if err != nil {
		log.Error("RPC address can't be identified:")
		panic(err)
	}

	//RPC client activation (to communicate with Kaspad)
	client, err := rpcclient.NewRPCClient(rpcAddress)
	if err != nil {
		log.Error("RPC client connection can't be activated:")
		panic(err)
	}

	client.SetTimeout(5 * time.Minute)

	//Fetch UTXOs from address
	availableUtxos, err := fetchAvailableUTXOs(client, myAddressString)
	if err != nil {
		log.Error("Available UTXOs can't be fetched:")
		panic(err)
	}

	//Define amount to send
	const balanceEpsilon = 10_000         // 10,000 sompi = 0.0001 kaspa
	const feeAmount = balanceEpsilon * 10 // use high fee amount, because can have a large number of inputs
	const sendAmount = balanceEpsilon * 1000
	totalSendAmount := uint64(sendAmount + feeAmount)

	//Select UTXOs matching Total Send amount
	selectedUTXOs, selectedValue, err := selectUTXOs(availableUtxos, totalSendAmount)
	if err != nil {
		log.Error("UTXOs can't be selected:")
		panic(err)
	}
	if len(selectedUTXOs) == 0 {
		log.Error("No UTXOs has been selected")
	}

	//Define change amount from selected UTXOs
	change := selectedValue - sendAmount - feeAmount

	//Generate transaction
	rpcTransaction, err := generateTransaction(myKeyPair, selectedUTXOs, sendAmount, change, recipientAddress, myAddress)
	if err != nil {
		log.Error("Transaction can't be correctly generated:")
		panic(err)
	}

	//Broadcast transaction
	transactionID, err := sendTransaction(client, rpcTransaction)
	if err != nil {
		log.Error("Transaction can't be correctly broadcasted:")
		panic(err)
	} else {
		log.Infof("Transaction has been successfully broadcasted: %s", transactionID)
	}

	// The End
	<-interrupt
	atomic.AddInt32(&shutdown, 1)
}

func parsePrivateKeyInKeyPair(privateKeyHex string) (*secp256k1.SchnorrKeyPair, *secp256k1.SchnorrPublicKey, error) {
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error parsing private key hex")
	}
	privateKey, err := secp256k1.DeserializeSchnorrPrivateKeyFromSlice(privateKeyBytes)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error deserializing private key")
	}
	publicKey, err := privateKey.SchnorrPublicKey()
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error generating public key")
	}
	return privateKey, publicKey, nil
}

// Collect spendable UTXOs from address
func fetchAvailableUTXOs(client *rpcclient.RPCClient, address string) (map[appmessage.RPCOutpoint]*appmessage.RPCUTXOEntry, error) {
	getUTXOsByAddressesResponse, err := client.GetUTXOsByAddresses([]string{address})
	if err != nil {
		return nil, err
	}
	dagInfo, err := client.GetBlockDAGInfo()
	if err != nil {
		return nil, err
	}

	spendableUTXOs := make(map[appmessage.RPCOutpoint]*appmessage.RPCUTXOEntry, 0)
	for _, entry := range getUTXOsByAddressesResponse.Entries {
		if !isUTXOSpendable(entry, dagInfo.VirtualDAAScore) {
			continue
		}
		spendableUTXOs[*entry.Outpoint] = entry.UTXOEntry
	}
	return spendableUTXOs, nil
}

// Verify UTXO is spendable (check if a minimum of 10 confirmations have been processed since UTXO creation)
func isUTXOSpendable(entry *appmessage.UTXOsByAddressesEntry, virtualSelectedParentBlueScore uint64) bool {
	blockDAAScore := entry.UTXOEntry.BlockDAAScore
	if !entry.UTXOEntry.IsCoinbase {
		const minConfirmations = 10
		return blockDAAScore+minConfirmations < virtualSelectedParentBlueScore
	}
	coinbaseMaturity := activeConfig().ActiveNetParams.BlockCoinbaseMaturity
	return blockDAAScore+coinbaseMaturity < virtualSelectedParentBlueScore
}

func selectUTXOs(utxos map[appmessage.RPCOutpoint]*appmessage.RPCUTXOEntry, amountToSend uint64) (
	selectedUTXOs []*appmessage.UTXOsByAddressesEntry, selectedValue uint64, err error) {

	selectedUTXOs = []*appmessage.UTXOsByAddressesEntry{}
	selectedValue = uint64(0)

	for outpoint, utxo := range utxos {
		outpointCopy := outpoint
		selectedUTXOs = append(selectedUTXOs, &appmessage.UTXOsByAddressesEntry{
			Outpoint:  &outpointCopy,
			UTXOEntry: utxo,
		})
		selectedValue += utxo.Amount

		if selectedValue >= amountToSend {
			break
		}

		const maxInputs = 100
		if len(selectedUTXOs) == maxInputs {
			log.Infof("Selected %d UTXOs so sending the transaction with %d sompis instead "+
				"of %d", maxInputs, selectedValue, amountToSend)
			break
		}
	}

	return selectedUTXOs, selectedValue, nil
}

// Generate transaction data
func generateTransaction(keyPair *secp256k1.SchnorrKeyPair, selectedUTXOs []*appmessage.UTXOsByAddressesEntry,
	sompisToSend uint64, change uint64, toAddress util.Address, fromAddress util.Address) (*appmessage.RPCTransaction, error) {

	// Generate transaction input from selectedUTXOs, collected from address query to Kaspad
	inputs := make([]*externalapi.DomainTransactionInput, len(selectedUTXOs))
	for i, utxo := range selectedUTXOs {
		outpointTransactionIDBytes, err := hex.DecodeString(utxo.Outpoint.TransactionID)
		if err != nil {
			return nil, err
		}
		outpointTransactionID, err := transactionid.FromBytes(outpointTransactionIDBytes)
		if err != nil {
			return nil, err
		}
		outpoint := externalapi.DomainOutpoint{
			TransactionID: *outpointTransactionID,
			Index:         utxo.Outpoint.Index,
		}
		utxoScriptPublicKeyScript, err := hex.DecodeString(utxo.UTXOEntry.ScriptPublicKey.Script)
		if err != nil {
			return nil, err
		}

		inputs[i] = &externalapi.DomainTransactionInput{
			PreviousOutpoint: outpoint,
			SigOpCount:       1,
			UTXOEntry: utxopkg.NewUTXOEntry(
				utxo.UTXOEntry.Amount,
				&externalapi.ScriptPublicKey{
					Script:  utxoScriptPublicKeyScript,
					Version: utxo.UTXOEntry.ScriptPublicKey.Version,
				},
				utxo.UTXOEntry.IsCoinbase,
				utxo.UTXOEntry.BlockDAAScore,
			),
		}
	}

	// Generate ScriptPublicKey for recipient address
	toScript, err := txscript.PayToAddrScript(toAddress)
	if err != nil {
		return nil, err
	}

	// Generate transaction output to pay recipient address
	mainOutput := &externalapi.DomainTransactionOutput{
		Value:           sompisToSend,
		ScriptPublicKey: toScript,
	}

	// Generate ScriptPublicKey for change address
	fromScript, err := txscript.PayToAddrScript(fromAddress)
	if err != nil {
		return nil, err
	}

	// Generate array of Outputs and add "change address output", in case change have to be sent back to recipient address
	outputs := []*externalapi.DomainTransactionOutput{mainOutput}
	if change > 0 {
		changeOutput := &externalapi.DomainTransactionOutput{
			Value:           change,
			ScriptPublicKey: fromScript,
		}
		outputs = append(outputs, changeOutput)
	}

	// Generate transaction data (not yet signed)
	domainTransaction := &externalapi.DomainTransaction{
		Version:      constants.MaxTransactionVersion,
		Inputs:       inputs,
		Outputs:      outputs,
		LockTime:     0,
		SubnetworkID: subnetworks.SubnetworkIDNative,
		Gas:          0,
		Payload:      nil,
	}

	// Sign all inputs in transaction
	for i, input := range domainTransaction.Inputs {
		signatureScript, err := txscript.SignatureScript(domainTransaction, i, consensushashing.SigHashAll, keyPair,
			&consensushashing.SighashReusedValues{})
		if err != nil {
			return nil, err
		}
		input.SignatureScript = signatureScript
	}

	// Convert transaction into a RPC transaction, ready to be broadcasted
	rpcTransaction := appmessage.DomainTransactionToRPCTransaction(domainTransaction)
	return rpcTransaction, nil
}

// Broadcast transaction on the network
func sendTransaction(client *rpcclient.RPCClient, rpcTransaction *appmessage.RPCTransaction) (string, error) {
	submitTransactionResponse, err := client.SubmitTransaction(rpcTransaction, false)
	if err != nil {
		return "", errors.Wrapf(err, "error submitting transaction")
	}
	return submitTransactionResponse.TransactionID, nil
}
