package signpdf

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	_ "crypto/sha512" // verification of other digests
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// The seal is a detached PKCS#7 signature (subfilter adbe.pkcs7.detached,
// SHA-256, RSA) over the whole file. It shows that a file has not changed
// since NeoCloud made it; it is not a PAdES or qualified signature.

// Signer holds the server's seal certificate.
type Signer struct {
	key    *rsa.PrivateKey
	cert   *x509.Certificate
	chain  []*x509.Certificate
	reason string
	name   string
	space  int // bytes reserved for the signature
}

// LoadSigner opens a PKCS#12 bundle, including the ones the Node server made.
func LoadSigner(p12 []byte, passphrase, reason, name string) (*Signer, error) {
	key, cert, chain, err := pkcs12.DecodeChain(p12, passphrase)
	if err != nil {
		return nil, refuse(CodeSigning, err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, refuse(CodeSigning, errors.New("the seal key is not an RSA key"))
	}
	space := max(8192, (len(p12)+2048+1023)/1024*1024)
	return &Signer{key: rsaKey, cert: cert, chain: chain, reason: reason, name: name, space: space}, nil
}

// GenerateP12 makes a self-signed RSA-2048 certificate in a PKCS#12 bundle
// protected by a random passphrase.
func GenerateP12(commonName string, years int) (p12 []byte, passphrase string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	if err != nil {
		return nil, "", err
	}
	subject := pkix.Name{CommonName: commonName, Organization: []string{"NeoCloud"}}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial.Add(serial, big.NewInt(1)), Subject: subject, Issuer: subject,
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(time.Duration(max(1, years)) * 365 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, "", err
	}
	secret := make([]byte, 24)
	if _, err = rand.Read(secret); err != nil {
		return nil, "", err
	}
	passphrase = base64.RawURLEncoding.EncodeToString(secret)
	p12, err = pkcs12.LegacyDES.Encode(key, cert, nil, passphrase)
	return p12, passphrase, err
}

var (
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidRSA           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidSHA256        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
)

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type issuerSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type signerInfo struct {
	Version            int
	Sid                issuerSerial
	DigestAlgorithm    pkix.AlgorithmIdentifier
	Attributes         asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
	Unsigned           asn1.RawValue `asn1:"optional,tag:1"`
}

type encapsulated struct{ Type asn1.ObjectIdentifier }

type signedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	Content          encapsulated
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	Signers          []signerInfo  `asn1:"set"`
}

type contentInfo struct {
	Type    asn1.ObjectIdentifier
	Content signedData `asn1:"explicit,tag:0"`
}

func attributeOf(kind asn1.ObjectIdentifier, value any) ([]byte, error) {
	encoded, err := asn1.Marshal(value)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(attribute{kind, []asn1.RawValue{{FullBytes: encoded}}})
}

func context0(content []byte) asn1.RawValue {
	return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: content}
}

// cms builds the detached signature over a digest.
func (s *Signer) cms(digest []byte, at time.Time) ([]byte, error) {
	var encoded [][]byte
	for _, item := range []struct {
		oid   asn1.ObjectIdentifier
		value any
	}{{oidContentType, oidData}, {oidSigningTime, at.UTC().Truncate(time.Second)}, {oidMessageDigest, digest}} {
		a, err := attributeOf(item.oid, item.value)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, a)
	}
	slices.SortFunc(encoded, bytes.Compare) // DER SET OF order
	attributes := bytes.Join(encoded, nil)
	set, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: attributes})
	if err != nil {
		return nil, err
	}
	hashed := sha256.Sum256(set)
	signature, err := rsa.SignPKCS1v15(nil, s.key, crypto.SHA256, hashed[:])
	if err != nil {
		return nil, err
	}
	certificates := slices.Clone(s.cert.Raw)
	for _, c := range s.chain {
		certificates = append(certificates, c.Raw...)
	}
	sha := pkix.AlgorithmIdentifier{Algorithm: oidSHA256, Parameters: asn1.NullRawValue}
	return asn1.Marshal(contentInfo{Type: oidSignedData, Content: signedData{
		Version: 1, DigestAlgorithms: []pkix.AlgorithmIdentifier{sha}, Content: encapsulated{oidData},
		Certificates: context0(certificates),
		Signers: []signerInfo{{
			Version: 1, Sid: issuerSerial{asn1.RawValue{FullBytes: s.cert.RawIssuer}, s.cert.SerialNumber},
			DigestAlgorithm: sha, Attributes: context0(attributes),
			SignatureAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidRSA, Parameters: asn1.NullRawValue}, Signature: signature,
		}},
	}})
}

const byteRangeBlank = "[0 0000000000 0000000000 0000000000]"

// serializeSealed adds an invisible signature field to the first page, writes
// the file and signs it.
func (d *document) serializeSealed(info types.Dict, s *Signer, at time.Time) ([]byte, error) {
	hexString := func(value string) string { return textString(value).PDFString() }
	blank := string(bytes.Repeat([]byte("0"), 2*s.space))
	body := "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange " + byteRangeBlank +
		" /Contents <" + blank + "> /Reason " + hexString(s.reason) + " /Name " + hexString(s.name) +
		" /M " + pdfDate(at).PDFString() + " >>"
	signature := d.add(types.Dict{})
	first := d.pages[0]
	widget := d.add(types.Dict{
		"Type": types.Name("Annot"), "Subtype": types.Name("Widget"), "FT": types.Name("Sig"), "T": textString("Signature1"),
		"Rect": types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)},
		"V":    signature, "F": types.Integer(132), "P": first.ref,
	})
	first.dict["Annots"] = append(append(types.Array{}, d.array(first.dict["Annots"])...), widget)
	d.root["AcroForm"] = types.Dict{"SigFlags": types.Integer(3), "Fields": types.Array{widget}}

	number := signature.ObjectNumber.Value()
	out, offsets, err := d.serialize(info, map[int]string{number: body})
	if err != nil {
		return nil, err
	}
	start := offsets[number]
	rangeAt := start + bytes.Index([]byte(body), []byte(byteRangeBlank))
	open := start + bytes.Index([]byte(body), []byte("/Contents <")) + len("/Contents ")
	end := open + 2*s.space + 2
	byteRange := fmt.Sprintf("[0 %d %d %d]", open, end, len(out)-end)
	if len(byteRange) > len(byteRangeBlank) {
		return nil, refuse(CodeSigning, errors.New("file too large to sign"))
	}
	copy(out[rangeAt:], byteRange+string(bytes.Repeat([]byte(" "), len(byteRangeBlank)-len(byteRange))))
	digest := sha256.New()
	digest.Write(out[:open])
	digest.Write(out[end:])
	cms, err := s.cms(digest.Sum(nil), at)
	if err != nil {
		return nil, refuse(CodeSigning, err)
	}
	if len(cms) > s.space {
		return nil, refuse(CodeSigning, errors.New("the signature does not fit"))
	}
	hex.Encode(out[open+1:], cms)
	return out, nil
}

var byteRangePattern = regexp.MustCompile(`^/ByteRange\s*\[\s*(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s*\]`)

// Verify checks the latest seal of a file: signed says whether it carries one,
// intact that the signed bytes still hash to the recorded digest, the RSA
// signature verifies and nothing was appended. It does not judge trust.
func Verify(data []byte) (signed, intact bool) {
	at := bytes.LastIndex(data, []byte("/ByteRange"))
	if at < 0 {
		return false, false
	}
	m := byteRangePattern.FindSubmatch(data[at:min(len(data), at+200)])
	if m == nil {
		return false, false
	}
	var r [4]int
	for i := range r {
		n, err := strconv.Atoi(string(m[i+1]))
		if err != nil || n < 0 || n > len(data) {
			return true, false
		}
		r[i] = n
	}
	if r[0] != 0 || r[1] <= 0 || r[2] < r[1]+2 || r[2]+r[3] != len(data) {
		return true, false
	}
	cms := make([]byte, (r[2]-r[1]-2)/2)
	if _, err := hex.Decode(cms, data[r[1]+1:r[1]+1+2*len(cms)]); err != nil {
		return true, false
	}
	var parsed contentInfo
	if _, err := asn1.Unmarshal(cms, &parsed); err != nil || len(parsed.Content.Signers) != 1 {
		return true, false
	}
	info := parsed.Content.Signers[0]
	var algorithm crypto.Hash
	switch {
	case info.DigestAlgorithm.Algorithm.Equal(oidSHA256):
		algorithm = crypto.SHA256
	case info.DigestAlgorithm.Algorithm.Equal(oidSHA384):
		algorithm = crypto.SHA384
	case info.DigestAlgorithm.Algorithm.Equal(oidSHA512):
		algorithm = crypto.SHA512
	default:
		return true, false
	}
	covered := algorithm.New()
	covered.Write(data[:r[1]])
	covered.Write(data[r[2]:])
	var recorded []byte
	for rest := info.Attributes.Bytes; len(rest) > 0; {
		var a attribute
		var err error
		if rest, err = asn1.Unmarshal(rest, &a); err != nil {
			return true, false
		}
		if a.Type.Equal(oidMessageDigest) && len(a.Values) == 1 {
			if _, err = asn1.Unmarshal(a.Values[0].FullBytes, &recorded); err != nil {
				return true, false
			}
		}
	}
	if !bytes.Equal(recorded, covered.Sum(nil)) {
		return true, false
	}
	// The signature covers the attributes re-encoded as a SET.
	set := slices.Clone(info.Attributes.FullBytes)
	set[0] = 0x31
	hashed := algorithm.New()
	hashed.Write(set)
	certificates, err := x509.ParseCertificates(parsed.Content.Certificates.Bytes)
	if err != nil {
		return true, false
	}
	for _, certificate := range certificates {
		if key, ok := certificate.PublicKey.(*rsa.PublicKey); ok && rsa.VerifyPKCS1v15(key, algorithm, hashed.Sum(nil), info.Signature) == nil {
			return true, true
		}
	}
	return true, false
}
