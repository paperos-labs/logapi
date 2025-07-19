package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"flag"
	"fmt"
	"hash"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/paperos-labs/logapi/csvpass"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

const (
	defaultIters      = 4096
	defaultSize       = 16
	defaultHash       = "SHA-256"
	defaultBcryptCost = 12
)

var (
	tsvFile = "credentials.tsv"
)

func main() {
	var subcmd string
	if len(os.Args) > 1 {
		subcmd = os.Args[1]
	}

	switch subcmd {
	case "set":
		handleSet(os.Args[2:])
	case "check":
		handleCheck(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "USAGE\n\tcsvpass [set|check] [--algorithm <plain|pbkdf2[,iters[,size[,hash]]]|bcrypt[,cost]] [--password] [--password-file <filepath>] <username>\n")
		os.Exit(1)
	}
}

func handleSet(args []string) {
	setFlags := flag.NewFlagSet("csvpass-set", flag.ExitOnError)
	algorithm := setFlags.String("algorithm", "pbkdf2", "Hash algorithm: plain, pbkdf2[,iters[,size[,hash]]], or bcrypt[,cost]")
	askPassword := setFlags.Bool("password", false, "Read password from stdin")
	passwordFile := setFlags.String("password-file", "", "Read password from file")
	setFlags.StringVar(&tsvFile, "tsv", tsvFile, "Credentials file to use")
	_ = setFlags.Parse(args)
	username := setFlags.Arg(0)
	if username == "id" {
		fmt.Fprintf(os.Stderr, "invalid username %q\n", username)
		os.Exit(1)
	}

	var pass string
	if len(*passwordFile) > 0 {
		data, err := os.ReadFile(*passwordFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password file: %v\n", err)
			os.Exit(1)
		}
		pass = strings.TrimSpace(string(data))
	} else if *askPassword {
		fmt.Fprintf(os.Stderr, "New Password: ")
		reader := bufio.NewReader(os.Stdin)
		data, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password from stdin: %v\n", err)
			os.Exit(1)
		}
		pass = strings.TrimSpace(data)
	} else {
		pass = generatePassword()
		fmt.Println(pass)
	}

	var challenge csvpass.Challenge
	algoParts := strings.Split(*algorithm, ",")
	switch algoParts[0] {
	case "plain":
		if len(algoParts) != 1 {
			fmt.Fprintf(os.Stderr, "invalid plain algorithm format: %q\n", *algorithm)
			os.Exit(1)
		}
		challenge.Params = []string{"plain"}
		challenge.Plain = pass
		h := sha256.Sum256([]byte(pass))
		challenge.Digest = h[:]
	case "pbkdf2":
		if len(algoParts) > 4 {
			fmt.Fprintf(os.Stderr, "invalid pbkdf2 algorithm format: %q\n", *algorithm)
			os.Exit(1)
		}
		iters := defaultIters
		if len(algoParts) > 1 {
			var err error
			iters, err = strconv.Atoi(algoParts[1])
			if err != nil || iters <= 0 {
				fmt.Fprintf(os.Stderr, "invalid iterations %q in %q\n", algoParts[1], *algorithm)
				os.Exit(1)
			}
		}
		size := defaultSize
		if len(algoParts) > 2 {
			var err error
			size, err = strconv.Atoi(algoParts[2])
			if err != nil || size < 8 || size > 32 {
				fmt.Fprintf(os.Stderr, "invalid size %q in %q\n", algoParts[2], *algorithm)
				os.Exit(1)
			}
		}
		hashName := defaultHash
		if len(algoParts) > 3 {
			if !slices.Contains([]string{"SHA-256", "SHA-1"}, algoParts[3]) {
				fmt.Fprintf(os.Stderr, "invalid hash %q in %q\n", algoParts[3], *algorithm)
				os.Exit(1)
			}
			hashName = algoParts[3]
		}
		challenge.Params = []string{"pbkdf2", strconv.Itoa(iters), strconv.Itoa(size), hashName}
		saltBytes := make([]byte, 16)
		_, _ = rand.Read(saltBytes)
		challenge.Salt = saltBytes
		var hasher func() hash.Hash
		switch hashName {
		case "SHA-1":
			hasher = sha1.New
		case "SHA-256":
			hasher = sha256.New
		default:
			fmt.Fprintf(os.Stderr, "invalid hash %q\n", hashName)
			os.Exit(1)
		}
		challenge.Digest = pbkdf2.Key([]byte(pass), saltBytes, iters, size, hasher)
	case "bcrypt":
		if len(algoParts) > 2 {
			fmt.Fprintf(os.Stderr, "invalid bcrypt algorithm format: %q\n", *algorithm)
			os.Exit(1)
		}
		cost := defaultBcryptCost
		if len(algoParts) > 1 {
			var err error
			cost, err = strconv.Atoi(algoParts[1])
			if err != nil || cost < 4 || cost > 31 {
				fmt.Fprintf(os.Stderr, "invalid bcrypt cost %q in %q\n", algoParts[1], *algorithm)
				os.Exit(1)
			}
		}
		challenge.Params = []string{"bcrypt"}
		digest, err := bcrypt.GenerateFromPassword([]byte(pass), cost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating bcrypt hash: %v\n", err)
			os.Exit(1)
		}
		challenge.Digest = digest
	default:
		fmt.Fprintf(os.Stderr, "invalid algorithm %q\n", algoParts[0])
		os.Exit(1)
	}

	f, err := os.Open(tsvFile)
	if err != nil {
		f, err = os.Create(tsvFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening/creating CSV: %v\n", err)
			os.Exit(1)
		}
	}
	defer func() { _ = f.Close() }()

	auth, err := csvpass.Load(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading CSV: %v\n", err)
		os.Exit(1)
	}

	_, exists := auth.Credentials[username]
	auth.Credentials[username] = challenge

	var records [][]string
	keys := slices.Sorted(maps.Keys(auth.Credentials))
	for _, id := range keys {
		c := auth.Credentials[id]
		record := c.ToRecord(id)
		records = append(records, record)
	}

	writeCSV(records)
	if exists {
		fmt.Fprintf(os.Stderr, "Wrote %q with new password for %q\n", tsvFile, username)
	} else {
		fmt.Fprintf(os.Stderr, "Added password for %q to %q\n", username, tsvFile)
	}
}

func handleCheck(args []string) {
	checkFlags := flag.NewFlagSet("csvpass-check", flag.ExitOnError)
	_ = checkFlags.Bool("password", true, "Read password from stdin")
	passwordFile := checkFlags.String("password-file", "", "Read password from file")
	checkFlags.StringVar(&tsvFile, "tsv", tsvFile, "Password file to use")
	_ = checkFlags.Parse(args)
	username := checkFlags.Arg(0)
	if username == "id" {
		fmt.Fprintf(os.Stderr, "invalid username %q\n", username)
		os.Exit(1)
	}

	var pass string
	if len(*passwordFile) > 0 {
		data, err := os.ReadFile(*passwordFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password file: %v\n", err)
			os.Exit(1)
		}
		pass = strings.TrimSpace(string(data))
	} else {
		fmt.Fprintf(os.Stderr, "Current Password: ")
		reader := bufio.NewReader(os.Stdin)
		data, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password from stdin: %v\n", err)
			os.Exit(1)
		}
		pass = strings.TrimSpace(data)
	}

	f, err := os.Open(tsvFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening CSV: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()

	auth, err := csvpass.Load(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading CSV: %v\n", err)
		os.Exit(1)
	}

	if auth.Verify(username, pass) {
		fmt.Println("verified")
		return
	}

	fmt.Fprintf(os.Stderr, "user '%s' not found or incorrect password\n", username)
	os.Exit(1)
}

func writeCSV(records [][]string) {
	f, err := os.Create(tsvFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating CSV: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()

	writer := csv.NewWriter(f)
	writer.Comma = '\t'

	_ = writer.Write([]string{"id", "algo", "salt", "digest"})
	for _, record := range records {
		_ = writer.Write(record)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing CSV: %v\n", err)
		os.Exit(1)
	}
}

func generatePassword() string {
	bytes := make([]byte, 12)
	_, _ = rand.Read(bytes)
	encoded := base64.RawURLEncoding.EncodeToString(bytes)
	parts := make([]string, 4)
	start := 0
	for i := range 4 {
		parts[i] = encoded[start : start+4]
		start += 4
	}
	return strings.Join(parts, "-")
}
