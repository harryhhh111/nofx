// claw402cost is a one-shot tool that totals how much USDC the NofxOS data
// payment channel (claw402) has spent, based on the application logs.
//
// It scans the log directories for claw402-data payment lines:
//   - old format: "💰 [claw402-data] Payment tx: 0x..." (tx hash only — the
//     amount is looked up on-chain via the Base RPC)
//   - new format: "💰 [claw402-data] Paid 0.012300 USDC for /api/... (tx 0x...)"
//     (amount is summed directly, no RPC call)
//
// Usage:
//
//	go run ./cmd/claw402cost [-dirs data,data-v2,logs] [-rpc https://mainnet.base.org]
//
// The tool is read-only: it scans logs and queries public chain data.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"nofx/wallet"
)

// keccak256("Transfer(address,address,uint256)")
const transferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

var (
	oldFormatRe = regexp.MustCompile(`\[claw402-data\] Payment tx: (0x[0-9a-fA-F]{64})`)
	newFormatRe = regexp.MustCompile(`\[claw402-data\] Paid ([0-9]+(?:\.[0-9]+)?) USDC`)
)

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type rpcReceipt struct {
	Result *struct {
		BlockNumber string `json:"blockNumber"`
		Logs        []struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
		} `json:"logs"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func main() {
	dirsFlag := flag.String("dirs", "data,data-v2,logs", "comma-separated log directories to scan")
	rpcURL := flag.String("rpc", wallet.BaseRPCURL, "Base chain JSON-RPC URL")
	flag.Parse()

	logFiles := collectLogFiles(strings.Split(*dirsFlag, ","))
	if len(logFiles) == 0 {
		fmt.Println("No log files found.")
		return
	}
	fmt.Printf("Scanning %d log files...\n", len(logFiles))

	txHashes := map[string]struct{}{}
	var directTotal float64
	var directCount int

	for _, f := range logFiles {
		scanLogFile(f, txHashes, &directTotal, &directCount)
	}

	hashes := make([]string, 0, len(txHashes))
	for h := range txHashes {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	fmt.Printf("Found %d historical payment txs (need on-chain lookup), %d new-format lines (amount in log).\n\n",
		len(hashes), directCount)

	client := &http.Client{Timeout: 15 * time.Second}
	var chainTotalUSDC float64
	var failed int

	for i, h := range hashes {
		amount, from, err := lookupPayment(client, *rpcURL, h)
		if err != nil {
			fmt.Printf("  %s  FAILED: %v\n", h, err)
			failed++
		} else {
			usdc := rawToUSDC(amount)
			chainTotalUSDC += usdc
			fmt.Printf("  %s  %.6f USDC  from %s\n", h, usdc, from)
		}
		if i < len(hashes)-1 {
			time.Sleep(100 * time.Millisecond) // be polite to the public RPC
		}
	}

	fmt.Println()
	fmt.Printf("On-chain (old format): %.6f USDC (%d txs, %d failed)\n", chainTotalUSDC, len(hashes)-failed, failed)
	fmt.Printf("Logged   (new format): %.6f USDC (%d payments)\n", directTotal, directCount)
	fmt.Printf("TOTAL: %.6f USDC (%d payments, %d failed)\n",
		chainTotalUSDC+directTotal, len(hashes)-failed+directCount, failed)
}

func collectLogFiles(dirs []string) []string {
	var files []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, "*.log"))
		if err != nil {
			continue
		}
		files = append(files, matches...)
	}
	return files
}

func scanLogFile(path string, txHashes map[string]struct{}, directTotal *float64, directCount *int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "[claw402-data]") {
			continue
		}
		if m := newFormatRe.FindStringSubmatch(line); m != nil {
			var v float64
			if _, err := fmt.Sscanf(m[1], "%f", &v); err == nil {
				*directTotal += v
				*directCount++
			}
			continue
		}
		if m := oldFormatRe.FindStringSubmatch(line); m != nil {
			txHashes[strings.ToLower(m[1])] = struct{}{}
		}
	}
}

// lookupPayment fetches the tx receipt and sums all USDC Transfer event values.
// Returns the raw (6-decimal) amount and the sender address.
func lookupPayment(client *http.Client, rpcURL, txHash string) (*big.Int, string, error) {
	reqBody, _ := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_getTransactionReceipt",
		Params:  []interface{}{txHash},
		ID:      1,
	})

	resp, err := client.Post(rpcURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	var receipt rpcReceipt
	if err := json.Unmarshal(respBody, &receipt); err != nil {
		return nil, "", fmt.Errorf("failed to parse receipt: %w", err)
	}
	if receipt.Error != nil {
		return nil, "", fmt.Errorf("rpc error: %s", receipt.Error.Message)
	}
	if receipt.Result == nil {
		return nil, "", fmt.Errorf("tx not found (no receipt)")
	}

	total := new(big.Int)
	from := ""
	for _, log := range receipt.Result.Logs {
		if !strings.EqualFold(log.Address, wallet.USDCContractBase) {
			continue
		}
		if len(log.Topics) < 3 || !strings.EqualFold(log.Topics[0], transferTopic) {
			continue
		}
		value := new(big.Int)
		value.SetString(strings.TrimPrefix(log.Data, "0x"), 16)
		total.Add(total, value)
		if from == "" {
			// topics[1] is the sender, left-padded to 32 bytes
			from = "0x" + log.Topics[1][len(log.Topics[1])-40:]
		}
	}
	if total.Sign() == 0 {
		return nil, "", fmt.Errorf("no USDC Transfer event in receipt")
	}
	return total, from, nil
}

func rawToUSDC(raw *big.Int) float64 {
	f := new(big.Float).SetInt(raw)
	f.Quo(f, big.NewFloat(1e6))
	v, _ := f.Float64()
	return v
}
