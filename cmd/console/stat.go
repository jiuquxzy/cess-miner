/*
	Copyright (C) CESS. All rights reserved.
	Copyright (C) Cumulus Encrypted Storage System. All rights reserved.

	SPDX-License-Identifier: Apache-2.0
*/

package console

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"

	cess "github.com/CESSProject/cess-go-sdk"
	"github.com/CESSProject/cess-go-sdk/core/pattern"
	sutils "github.com/CESSProject/cess-go-sdk/utils"
	"github.com/CESSProject/cess-miner/configs"
	"github.com/CESSProject/p2p-go/out"
	"github.com/btcsuite/btcutil/base58"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// Query miner state
func Command_State_Runfunc(cmd *cobra.Command, args []string) {
	cfg, err := buildAuthenticationConfig(cmd)
	if err != nil {
		out.Err(err.Error())
		os.Exit(1)
	}

	cli, err := cess.New(
		context.Background(),
		cess.Name(configs.Name),
		cess.ConnectRpcAddrs(cfg.ReadRpcEndpoints()),
		cess.Mnemonic(cfg.ReadMnemonic()),
		cess.TransactionTimeout(configs.TimeToWaitEvent),
	)
	if err != nil {
		out.Err(err.Error())
		os.Exit(1)
	}
	defer cli.Close()

	// query your own information on the chain
	minerInfo, err := cli.QueryStorageMiner(cli.GetSignatureAccPulickey())
	if err != nil {
		if err.Error() != pattern.ERR_Empty {
			out.Err(pattern.ERR_RPC_CONNECTION.Error())
		} else {
			out.Err("signature account does not exist, possible: 1.balance is empty 2.rpc address error")
		}
		os.Exit(1)
	}

	minerInfo.Collaterals.Div(new(big.Int).SetBytes(minerInfo.Collaterals.Bytes()), big.NewInt(configs.TokenTCESS))

	beneficiaryAcc, _ := sutils.EncodePublicKeyAsCessAccount(minerInfo.BeneficiaryAccount[:])

	name := cli.GetSDKName()
	if strings.Contains(name, "bucket") {
		name = "storage miner"
	}

	startBlock, err := cli.QueryStorageMinerStakingStartBlock(cli.GetSignatureAccPulickey())
	if err != nil {
		if err.Error() != pattern.ERR_Empty {
			out.Err(pattern.ERR_RPC_CONNECTION.Error())
			os.Exit(1)
		} else {
			out.Err("your staking starting block is not found")
		}
	}

	var stakingAcc = cfg.ReadStakingAcc()
	if stakingAcc == "" {
		stakingAcc = cli.GetSignatureAcc()
	}

	var tableRows = []table.Row{
		{"name", name},
		{"peer id", base58.Encode([]byte(string(minerInfo.PeerId[:])))},
		{"state", string(minerInfo.State)},
		{"staking amount", fmt.Sprintf("%v %s", minerInfo.Collaterals, cli.GetTokenSymbol())},
		{"staking start", startBlock},
		{"debt amount", fmt.Sprintf("%v %s", minerInfo.Debt, cli.GetTokenSymbol())},
		{"declaration space", unitConversion(minerInfo.DeclarationSpace)},
		{"validated space", unitConversion(minerInfo.IdleSpace)},
		{"used space", unitConversion(minerInfo.ServiceSpace)},
		{"locked space", unitConversion(minerInfo.LockSpace)},
		{"signature account", cli.GetSignatureAcc()},
		{"staking account", stakingAcc},
		{"earnings account", beneficiaryAcc},
	}
	tw := table.NewWriter()
	tw.AppendRows(tableRows)
	fmt.Println(tw.Render())
	os.Exit(0)
}

func unitConversion(value types.U128) string {
	var result string
	if value.IsUint64() {
		v := value.Uint64()
		if v >= (pattern.SIZE_1GiB * 1024 * 1024 * 1024) {
			result = fmt.Sprintf("%.2f EiB", float64(float64(v)/float64(pattern.SIZE_1GiB*1024*1024*1024)))
			return result
		}
		if v >= (pattern.SIZE_1GiB * 1024 * 1024) {
			result = fmt.Sprintf("%.2f PiB", float64(float64(v)/float64(pattern.SIZE_1GiB*1024*1024)))
			return result
		}
		if v >= (pattern.SIZE_1GiB * 1024) {
			result = fmt.Sprintf("%.2f TiB", float64(float64(v)/float64(pattern.SIZE_1GiB*1024)))
			return result
		}
		if v >= (pattern.SIZE_1GiB) {
			result = fmt.Sprintf("%.2f GiB", float64(float64(v)/float64(pattern.SIZE_1GiB)))
			return result
		}
		if v >= (pattern.SIZE_1MiB) {
			result = fmt.Sprintf("%.2f MiB", float64(float64(v)/float64(pattern.SIZE_1MiB)))
			return result
		}
		if v >= (pattern.SIZE_1KiB) {
			result = fmt.Sprintf("%.2f KiB", float64(float64(v)/float64(pattern.SIZE_1KiB)))
			return result
		}
		result = fmt.Sprintf("%v Bytes", v)
		return result
	}
	v := new(big.Int).SetBytes(value.Bytes())
	v.Quo(v, new(big.Int).SetUint64((pattern.SIZE_1GiB * 1024 * 1024 * 1024)))
	result = fmt.Sprintf("%v EiB", v)
	return result
}
