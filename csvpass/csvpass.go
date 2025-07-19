package csvpass

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"hash"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

type Username = string

// Challenge represents a row in the CSV file
type Challenge struct {
	Plain  string
	Params []string
	Salt   []byte
	Digest []byte
}

func (c Challenge) ToRecord(id string) []string {
	var paramList, salt, digest string

	paramList = strings.Join(c.Params, ",")
	switch c.Params[0] {
	case "plain":
		digest = c.Plain
	case "pbkdf2":
		salt = base64.RawURLEncoding.EncodeToString(c.Salt)
		digest = base64.RawURLEncoding.EncodeToString(c.Digest)
	case "bcrypt":
		digest = string(c.Digest)
	}

	return []string{id, paramList, salt, digest}
}

// Auth holds user credentials
type Auth struct {
	Credentials map[Username]Challenge
}

// Load reads credentials from the given path
func Load(f *os.File) (*Auth, error) {
	auth := &Auth{Credentials: make(map[Username]Challenge)}

	csvr := csv.NewReader(f)
	csvr.Comma = '\t'
	_, _ = csvr.Read() // strip header row
	for {
		record, err := csvr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if len(record) == 0 {
			continue
		}

		if len(record) == 1 {
			if len(record[0]) == 0 {
				continue
			}
		}

		if len(record) != 4 {
			return nil, fmt.Errorf("invalid %q format: %#v (%d)", f.Name(), record, len(record))
		}

		username, paramList, salt64, secret := record[0], record[1], record[2], record[3]

		var challenge Challenge
		challenge.Params = strings.Split(paramList, ",")
		if len(challenge.Params) == 0 {
			fmt.Fprintf(os.Stderr, "no algorithm parameters for %q\n", username)
		}

		switch challenge.Params[0] {
		case "plain":
			if len(challenge.Params) > 1 {
				return nil, fmt.Errorf("invalid plain parameters %#v", challenge.Params)
			}

			challenge.Plain = secret
			h := sha256.Sum256([]byte(secret))
			challenge.Digest = h[:]
		case "pbkdf2":
			var err error

			challenge.Salt, err = base64.RawURLEncoding.DecodeString(salt64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not decode salt %q for %q\n", salt64, username)
			}

			challenge.Digest, err = base64.RawURLEncoding.DecodeString(secret)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not decode digest %q for %q\n", secret, username)
			}

			iters, err := strconv.Atoi(challenge.Params[1])
			if err != nil {
				return nil, err
			}
			if iters <= 0 {
				return nil, fmt.Errorf("invalid iterations %s", challenge.Params[1])
			}

			size, err := strconv.Atoi(challenge.Params[2])
			if err != nil {
				return nil, err
			}
			if size < 8 || size > 32 {
				return nil, fmt.Errorf("invalid size %s", challenge.Params[2])
			}

			if !slices.Contains([]string{"SHA-256", "SHA-1"}, challenge.Params[3]) {
				return nil, fmt.Errorf("invalid hash %s", challenge.Params[3])
			}
		case "bcrypt":
			if len(challenge.Params) > 1 {
				return nil, fmt.Errorf("invalid bcrypt parameters %#v", challenge.Params)
			}

			challenge.Digest = []byte(secret)
		default:
			return nil, fmt.Errorf("invalid algorithm %s", challenge.Params[0])
		}

		auth.Credentials[username] = challenge
	}

	return auth, nil
}

// Verify checks Basic Auth credentials
func (a Auth) Verify(username, password string) bool {
	challenge, ok := a.Credentials[username]
	if !ok {
		return false
	}

	var digest []byte
	switch challenge.Params[0] {
	case "plain":
		h := sha256.Sum256([]byte(password))
		digest = h[:]
	case "pbkdf2":
		// these are checked on load
		iters, _ := strconv.Atoi(challenge.Params[1])
		size, _ := strconv.Atoi(challenge.Params[2])
		var hasher func() hash.Hash
		switch challenge.Params[3] {
		case "SHA-1":
			hasher = sha1.New
		case "SHA-256":
			hasher = sha256.New
		default:
			panic(fmt.Errorf("invalid hash %q", challenge.Params[3]))
		}
		h := pbkdf2.Key([]byte(password), challenge.Salt, iters, size, hasher)
		digest = h
	case "bcrypt":
		err := bcrypt.CompareHashAndPassword(challenge.Digest, []byte(password))
		return err == nil
	}

	return bytes.Equal(challenge.Digest, digest)
}
