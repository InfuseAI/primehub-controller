package license

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
)

const PUBLIC_PEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAtIEgZLyg+tMgo08potRe
dgxs98M+p29J6WXOiOOF50b/JF7bvDutEmBLAvRN3DkA2Pwhu1llDVzyXnVZjFkO
br9/8NBr86NuHN9zT6mudF0hBViQlQa94lUp5I+CoZg3gB6238rkvXgG1S/TO9or
3QrqbT1ZS1uU8hIqg9aV//QEeUtOfXNoETwt34DhLqzjXnNHyW24ODpYkIzgGpbV
4RgitBExmrrAnnFMgfK7seWeu+H7s1ZME6aQ49DuxiFHXTAcG7zRO0m4PJ1yZTTF
3abgThdEfY3BsFeP+E0zLSIPL2vvqLbEReWlZ4JqG59G91w+eAFZtpq4Mw4s3ihU
SQIDAQAB
-----END PUBLIC KEY-----`

func Verify(signed_license string) (ok bool, err error) {
	splits := strings.Split(signed_license, ".")
	if len(splits) != 2 {
		return false, errors.New("malformed signed license key")
	}

	message, sig := splits[0], splits[1]
	hashed := sha256.Sum256([]byte(message))
	block, _ := pem.Decode([]byte(PUBLIC_PEM))
	pub, _ := x509.ParsePKIXPublicKey(block.Bytes)
	pubKey, _ := pub.(*rsa.PublicKey)

	signature, err := hex.DecodeString(sig)
	if err != nil {
		return false, errors.New("decode license signature failed: " + err.Error())
	}

	err = rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed[:], signature)
	if err != nil {
		return false, errors.New("license verification failed: " + err.Error())
	} else {
		return true, nil
	}
}
