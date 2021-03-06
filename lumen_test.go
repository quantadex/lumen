package lumen

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/0xfe/lumen/cli"
	"github.com/sirupsen/logrus"
)

// Send some funds here if friendbot works
const payBack = "GCXZW4IEBTCQQ6JY4COH3O2SSCBUAMPJ4WM4EU2GWBZ4MNVZJSTISBOE"

// Suck funds from here if friendbot fails
const fundSource = "SDPWNPMCESNRW47YS2XIZ3BZTGTGBO54A3EPGUG72DYPQJO5MAEGK6JY"

func getTempFile() (string, func()) {
	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		log.Fatal(err)
	}

	file := dir + string(os.PathSeparator) + "lumen-integration-test.json"

	return file, func() {
		logrus.Debugf("cleaning up temp file: %s", file)
		os.RemoveAll(dir)
	}
}

func run(cli *cli.CLI, command string) string {
	fmt.Printf("$ lumen %s\n", command)
	got := cli.TestCommand(command)
	fmt.Printf("%s\n", got)
	return strings.TrimSpace(got)
}

func runArgs(cli *cli.CLI, args ...string) string {
	fmt.Printf("$ lumen %s\n", strings.Join(args, " "))
	got := cli.Embeddable().Run(args...)
	fmt.Printf("%s\n", got)
	return strings.TrimSpace(got)
}

func expectOutput(t *testing.T, cli *cli.CLI, want string, command string) {
	got := run(cli, command)

	if got != want {
		t.Errorf("(%s) wrong output: want %v, got %v", command, want, got)
	}
}

func newCLI() (*cli.CLI, func()) {
	file, cleanupFunc := getTempFile()
	os.Setenv("LUMEN_STORE", "file,"+file)

	lumen := cli.NewCLI()
	lumen.TestCommand("version")
	lumen.TestCommand("ns test")
	lumen.TestCommand("set config:network test")
	run(lumen, fmt.Sprintf("account set landlord %s", payBack))
	run(lumen, fmt.Sprintf("account set sugar %s", fundSource))

	return lumen, cleanupFunc
}

func getBalance(cli *cli.CLI, account string) float64 {
	balanceString := run(cli, "balance "+account)

	balance, err := strconv.ParseFloat(balanceString, 64)

	if err != nil {
		return 0
	}

	return balance
}

// Create new funded test account
func createFundedAccount(t *testing.T, cli *cli.CLI, name string) {
	run(cli, "account new "+name)
	run(cli, "friendbot "+name)

	balance := getBalance(cli, name)
	if balance > 100 {
		// return some balance to landlord
		run(cli, fmt.Sprintf("pay 5000 --from %s --to landlord", name))
	} else {
		// friendbot failed, fund via sugar daddy
		run(cli, fmt.Sprintf("pay 1000 --from sugar --to %s --fund", name))
	}

	balance = getBalance(cli, name)
	if balance < 999 {
		t.Fatalf("could not fund account: %s", name)
	}
}

func TestPayments(t *testing.T) {
	cli, cleanupFunc := newCLI()
	defer cleanupFunc()

	createFundedAccount(t, cli, "mo")
	run(cli, "account new kelly")

	expectOutput(t, cli, "", "pay 100 --from mo --to kelly --memotext hi --fund")
	expectOutput(t, cli, "", "pay 1 --from kelly --to mo --memotext yo -v")

	balance := getBalance(cli, "kelly")

	if balance > 99 {
		t.Fatalf("expected balance <= 99 got %v", balance)
	}
}

func TestAssets(t *testing.T) {
	cli, cleanupFunc := newCLI()
	defer cleanupFunc()

	createFundedAccount(t, cli, "mo")

	// Create a USD asset issued by citibank
	run(cli, "account new citibank")
	expectOutput(t, cli, "", "pay 100 --from mo --to citibank --memoid 1 --fund")
	run(cli, "asset set USD citibank")

	// Create a trustline between kelly and citibank with a $1000 limit, then
	// send her $100
	run(cli, "account new kelly")
	expectOutput(t, cli, "", "pay 10 --from mo --to kelly --memotext initial --fund")

	expectOutput(t, cli, "", "trust create kelly USD 1000")
	expectOutput(t, cli, "", "pay 100 USD --from citibank --to kelly")

	// Verify balance on kelly's account
	expectOutput(t, cli, "100.0000000", "balance kelly USD")

	// Change the flags on the issuers account
	expectOutput(t, cli, "", "flags citibank auth_revocable")
	expectOutput(t, cli, "", "flags citibank auth_revocable --clear")
}

func TestMultisig(t *testing.T) {
	cli, cleanupFunc := newCLI()
	defer cleanupFunc()

	createFundedAccount(t, cli, "mo")

	// Create four accounts
	run(cli, "account new sharon")
	run(cli, "account new bob")
	run(cli, "account new mary")
	run(cli, "account new fred")

	expectOutput(t, cli, "", "pay 100 --from mo --to sharon --memoid 1 --fund")

	/*
		// Start watching sharon's ledger
		address := run(cli, "account address sharon")
		var done func()
		go func(address string) {
			cli, cleanupFunc := newCLI()
			defer cleanupFunc()
			done = cli.StopWatcher

			run(cli, "watch -v --cursor start --format struct "+address)
		}(address)
	*/

	expectOutput(t, cli, "", "pay 100 --from mo --to bob --memoid 1 --fund")
	expectOutput(t, cli, "", "pay 100 --from mo --to mary --memoid 1 --fund")
	expectOutput(t, cli, "", "pay 100 --from mo --to fred --memoid 1 --fund")

	// Add bob and mary as sharon's signers
	expectOutput(t, cli, "", "signer add bob 1 --to sharon")
	expectOutput(t, cli, "", "signer add mary 1 --to sharon")

	// Raise the signing thresholds for sharon
	expectOutput(t, cli, "", "signer thresholds sharon 2 2 2")

	// Make a multisig payment
	expectOutput(t, cli, "", "pay 10 --from sharon --to fred --signers bob,mary")

	// Remove Bob as a signer (also multisig)
	expectOutput(t, cli, "", "signer remove bob --from sharon --signers sharon,bob")

	// Make another multisig payment
	expectOutput(t, cli, "", "pay 10 --from sharon --to fred --signers sharon,mary")

	// Kill sharon's keys
	expectOutput(t, cli, "", "signer thresholds sharon 1 1 1 --signers sharon,mary")
	expectOutput(t, cli, "", "signer masterweight sharon 0")

	expectOutput(t, cli, "error", "pay 10 --from sharon --to fred")
	expectOutput(t, cli, "", "pay 10 --from sharon --to fred --signers mary")

	// Stop watching sharon's ledger
	// done()
}

func TestDex(t *testing.T) {
	cli, cleanupFunc := newCLI()
	defer cleanupFunc()

	// Get funds from friendbot
	createFundedAccount(t, cli, "mo")

	run(cli, "account new issuer")
	run(cli, "account new citibank")
	run(cli, "account new chase")
	run(cli, "account new bob")

	// Fund new accounts via mo
	expectOutput(t, cli, "", "pay 100 --from mo --to issuer --memoid 1 --fund")
	expectOutput(t, cli, "", "pay 100 --from mo --to citibank --memoid 1 --fund")
	expectOutput(t, cli, "", "pay 100 --from mo --to chase --memoid 1 --fund")
	expectOutput(t, cli, "", "pay 100 --from mo --to bob --memoid 1 --fund")

	// Create new assets
	run(cli, "asset set XLM issuer --type native")
	run(cli, "asset set USD issuer")
	run(cli, "asset set EUR issuer")

	// Create a trustlines and issue funds
	for _, account := range []string{"mo", "bob", "citibank", "chase"} {
		expectOutput(t, cli, "", fmt.Sprintf("trust create %s USD 1000000", account))
		expectOutput(t, cli, "", fmt.Sprintf("pay 100000 USD --from issuer --to %s", account))
		expectOutput(t, cli, "", fmt.Sprintf("trust create %s EUR 1000000", account))
		expectOutput(t, cli, "", fmt.Sprintf("pay 100000 EUR --from issuer --to %s", account))
	}

	// Create two offers at different prices
	expectOutput(t, cli, "", "dex trade mo --sell USD --buy EUR --amount 5 --price 1")
	expectOutput(t, cli, "", "dex trade bob --sell USD --buy EUR --amount 5 --price 2")

	// List their transactions
	out := run(cli, "dex list mo")
	if out == "" {
		t.Errorf("unexpected result, want offers, got nothing")
	}

	out = run(cli, "dex list bob")
	if out == "" {
		t.Errorf("unexpected result, want offers, got nothing")
	}

	// Make sure orderbook has entries
	out = run(cli, "dex orderbook USD EUR")
	if out == "" {
		t.Errorf("unexpected result, want orderbook, got nothing")
	}

	out = run(cli, "dex orderbook EUR USD --limit 0")
	if out != "" {
		t.Errorf("unexpected result, want empty string, got: %v", out)
	}

	// Create counterparty offers
	expectOutput(t, cli, "", "dex trade citibank --sell EUR --buy USD --amount 10 --price 0.5")
	expectOutput(t, cli, "", "dex trade chase --sell EUR --buy USD --amount 2 --price 1")

	run(cli, "dex list mo")
	run(cli, "dex list bob")

	expectOutput(t, cli, "99995.0000000", "balance mo USD")
	expectOutput(t, cli, "100005.0000000", "balance bob EUR")

	// Try a path payment (automatic)
	expectOutput(t, cli, "", "dex trade citibank --sell USD --buy XLM --amount 10 --price 1")
	expectOutput(t, cli, "", "pay 1 EUR --to bob --from mo --with XLM --max 20")

	expectOutput(t, cli, "100006.0000000", "balance bob EUR")

	// Try a path payment (manual)
	expectOutput(t, cli, "", "dex trade citibank --sell USD --buy XLM --amount 10 --price 1")
	expectOutput(t, cli, "", "pay 1 EUR --to bob --from mo --with XLM --max 20 --path USD,EUR -v")

}

func TestData(t *testing.T) {
	cli, cleanupFunc := newCLI()
	defer cleanupFunc()

	createFundedAccount(t, cli, "mo")

	expectOutput(t, cli, "error", "data mo foo")
	expectOutput(t, cli, "", "data mo foo bar")
	expectOutput(t, cli, "bar", "data mo foo")
	expectOutput(t, cli, "", "data mo foo --clear")
	expectOutput(t, cli, "error", "data mo foo")
}

func TestTimeBounds(t *testing.T) {
	cli, cleanupFunc := newCLI()
	defer cleanupFunc()

	createFundedAccount(t, cli, "mo")
	createFundedAccount(t, cli, "bob")

	cli.Embeddable()
	output := runArgs(cli, "pay", "1", "--from", "mo", "--to", "bob", "--mintime", "2017-01-01 12:00:00")
	if output != "error" {
		t.Errorf("want error, got %v", output)
	}
	output = runArgs(cli, "pay", "1", "--from", "mo", "--to", "bob", "--maxtime", "2017-01-01 12:00:00")
	if output != "error" {
		t.Errorf("want error, got %v", output)
	}
	output = runArgs(cli, "pay", "1", "--from", "mo", "--to", "bob", "--mintime", "2060-01-01 12:00:00", "--maxtime", "2075-01-01 12:00:00")
	if output != "error" {
		t.Errorf("want error, got %v", output)
	}
	output = runArgs(cli, "pay", "1", "--from", "mo", "--to", "bob", "--mintime", "2006-01-01 12:00:00", "--maxtime", "2075-01-01 12:00:00")
	if output != "" {
		t.Errorf("want nothing, got %v", output)
	}

	log.Print(output)
}
