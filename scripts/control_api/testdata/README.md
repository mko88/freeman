# Test fixtures for the local server

Two things `scripts/control_api/server.py` needs that the Python standard
library can't produce on its own. Both are test-only and committed so the
suite stays stdlib-only and offline.

## The certificate pairs

`server.pem`/`server.key` is what the TLS servers present: self-signed,
`CN=freeman-test-server`, valid for `localhost`, `127.0.0.1` and `::1`.
Being self-signed is the point — it's what makes "Skip TLS certificate
check" the difference between a request that works and one that doesn't.

`client.pem`/`client.key` is the pair Freeman presents to the mTLS
server, which trusts that certificate directly (a self-signed
certificate is its own issuer).

These are not secrets. They authenticate nothing but a loopback listener
this suite starts and stops itself, and the private keys are in the repo
precisely so anyone can run the suite without generating anything.

They expire on 2120-01-01.

## `BROTLI_JSON`

Python can decompress brotli but not compress it, so the `/brotli`
endpoint serves a stream produced once by Go's `github.com/andybalholm/
brotli` — the same library Freeman decodes with.

## Regenerating

Both come from one throwaway program. Save it at the repo root as
`gen_fixtures.go`, run it, and delete it again:

```go
//go:build ignore

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/andybalholm/brotli"
)

func write(path string, block *pem.Block) {
	var buf bytes.Buffer
	if err := pem.Encode(&buf, block); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
}

func makeCert(dir, name, cn string, client bool) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"Freeman control-API test suite"}},
		NotBefore:             time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2120, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if client {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}
	write(dir+"/"+name+".pem", &pem.Block{Type: "CERTIFICATE", Bytes: der})
	write(dir+"/"+name+".key", &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	fmt.Println("wrote", name)
}

func main() {
	dir := os.Args[1]
	makeCert(dir, "server", "freeman-test-server", false)
	makeCert(dir, "client", "freeman-test-client", true)

	payload := []byte(`{"brotli": true, "method": "GET", "origin": "127.0.0.1"}`)
	var out bytes.Buffer
	w := brotli.NewWriterLevel(&out, brotli.BestCompression)
	if _, err := w.Write(payload); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("BROTLI_JSON = %q\n", base64.StdEncoding.EncodeToString(out.Bytes()))
}
```

```
go run gen_fixtures.go scripts/control_api/testdata
```

Then paste the printed `BROTLI_JSON` into `server.py` (and update
`BROTLI_PAYLOAD` if you changed the payload).
