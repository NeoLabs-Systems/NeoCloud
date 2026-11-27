package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	blobHeaderLength = 20
	blobTagLength    = 16
	segmentSize      = 64 * 1024
	blobVersion      = 1
)

var blobMagic = [4]byte{'N', 'C', 'B', '1'}

// EncryptedSize reports the Node server's on-disk size for plaintext size.
func EncryptedSize(size int64) (int64, error) {
	if size < 0 || size > math.MaxInt64-blobHeaderLength-blobTagLength {
		return 0, errors.New("cryptox: invalid plaintext size")
	}
	count := int64(1)
	if size > 0 {
		count = (size + segmentSize - 1) / segmentSize
	}
	extra := count * blobTagLength
	if size > math.MaxInt64-blobHeaderLength-extra {
		return 0, errors.New("cryptox: encrypted size overflow")
	}
	return blobHeaderLength + size + extra, nil
}

func blobKey(dek []byte, blobID string) ([]byte, error) {
	if len(dek) != keyLength {
		return nil, fmt.Errorf("cryptox: account data key must be %d bytes", keyLength)
	}
	return hkdf.Key(sha256.New, dek, []byte(blobID), "neocloud:blob", keyLength)
}

func makeBlobHeader(prefix []byte) []byte {
	header := make([]byte, blobHeaderLength)
	copy(header[:4], blobMagic[:])
	header[4] = blobVersion
	binary.BigEndian.PutUint32(header[8:12], segmentSize)
	copy(header[12:], prefix)
	return header
}

func parseBlobHeader(header []byte) ([]byte, error) {
	if len(header) != blobHeaderLength || string(header[:4]) != string(blobMagic[:]) ||
		header[4] != blobVersion || binary.BigEndian.Uint32(header[8:12]) != segmentSize {
		return nil, errors.New("Blob header is missing or damaged.")
	}
	return header[12:20], nil
}

func segmentNonce(prefix []byte, index uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[8:], index)
	return nonce
}

func segmentAAD(header []byte, index uint32, final bool) []byte {
	aad := make([]byte, blobHeaderLength+5)
	copy(aad, header)
	binary.BigEndian.PutUint32(aad[blobHeaderLength:], index)
	if final {
		aad[blobHeaderLength+4] = 1
	}
	return aad
}

// EncryptBlob writes the compatible NCB1 format while holding at most two
// 64 KiB plaintext segments in memory. The output must be moved into place
// only after this function succeeds.
func EncryptBlob(dst io.Writer, src io.Reader, dek []byte, blobID string) (int64, error) {
	key, err := blobKey(dek, blobID)
	if err != nil {
		return 0, err
	}
	prefix := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, prefix); err != nil {
		return 0, fmt.Errorf("cryptox: generate blob nonce: %w", err)
	}
	header := makeBlobHeader(prefix)
	written, err := writeAll(dst, header)
	if err != nil {
		return written, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return written, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return written, err
	}

	current := make([]byte, segmentSize)
	currentN, currentErr := io.ReadFull(src, current)
	if currentErr != nil && currentErr != io.EOF && currentErr != io.ErrUnexpectedEOF {
		return written, fmt.Errorf("cryptox: read plaintext: %w", currentErr)
	}
	index := uint32(0)
	if currentN == 0 && currentErr == io.EOF {
		sealed := aead.Seal(nil, segmentNonce(prefix, index), nil, segmentAAD(header, index, true))
		count, writeErr := writeAll(dst, sealed)
		return written + count, writeErr
	}
	if currentErr == io.ErrUnexpectedEOF {
		sealed := aead.Seal(nil, segmentNonce(prefix, index), current[:currentN], segmentAAD(header, index, true))
		count, writeErr := writeAll(dst, sealed)
		return written + count, writeErr
	}

	for {
		// A full segment is final only if no further plaintext follows. Read
		// ahead one segment to determine the final-bit AAD unambiguously.
		next := make([]byte, segmentSize)
		nextN, nextErr := io.ReadFull(src, next)
		if nextErr != nil && nextErr != io.EOF && nextErr != io.ErrUnexpectedEOF {
			return written, fmt.Errorf("cryptox: read plaintext: %w", nextErr)
		}
		final := nextN == 0 && nextErr == io.EOF
		sealed := aead.Seal(nil, segmentNonce(prefix, index), current[:currentN], segmentAAD(header, index, final))
		count, writeErr := writeAll(dst, sealed)
		written += count
		if writeErr != nil || final {
			return written, writeErr
		}
		if index == math.MaxUint32 {
			return written, errors.New("cryptox: blob contains too many segments")
		}
		index++
		current = next
		currentN = nextN
		if nextErr == io.ErrUnexpectedEOF {
			sealed = aead.Seal(nil, segmentNonce(prefix, index), current[:currentN], segmentAAD(header, index, true))
			count, writeErr = writeAll(dst, sealed)
			return written + count, writeErr
		}
	}
}

// DecryptBlobRange writes plaintext bytes [start,end], inclusive. Only the
// segments touched by the range are read, matching NeoCloud's HTTP range path.
func DecryptBlobRange(src io.ReaderAt, size, start, end int64, dek []byte, blobID string, dst io.Writer) (int64, error) {
	if size < 0 || size > math.MaxInt64-blobHeaderLength-blobTagLength {
		return 0, errors.New("Invalid blob size.")
	}
	if size > 0 && (start < 0 || start > end || end >= size) {
		return 0, errors.New("Invalid byte range.")
	}
	header := make([]byte, blobHeaderLength)
	if _, err := src.ReadAt(header, 0); err != nil {
		return 0, errors.New("Blob is shorter than its recorded size.")
	}
	prefix, err := parseBlobHeader(header)
	if err != nil {
		return 0, err
	}
	key, err := blobKey(dek, blobID)
	if err != nil {
		return 0, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return 0, err
	}
	last := uint64(0)
	if size > 0 {
		last = uint64((size - 1) / segmentSize)
	}
	firstIndex, finalIndex := uint64(0), last
	if size > 0 {
		firstIndex = uint64(start / segmentSize)
		finalIndex = uint64(end / segmentSize)
	}
	if finalIndex > math.MaxUint32 {
		return 0, errors.New("Blob format supports at most 2^32 segments.")
	}
	var produced int64
	for i := firstIndex; i <= finalIndex; i++ {
		plainStart := int64(i) * segmentSize
		plainLen := int64(segmentSize)
		if size-plainStart < plainLen {
			plainLen = size - plainStart
		}
		if size == 0 {
			plainLen = 0
		}
		cipherOffset := int64(blobHeaderLength) + int64(i)*int64(segmentSize+blobTagLength)
		sealed := make([]byte, int(plainLen)+blobTagLength)
		if _, err := src.ReadAt(sealed, cipherOffset); err != nil {
			return produced, errors.New("Blob is shorter than its recorded size.")
		}
		plain, err := aead.Open(nil, segmentNonce(prefix, uint32(i)), sealed, segmentAAD(header, uint32(i), i == last))
		if err != nil {
			return produced, fmt.Errorf("cryptox: authenticate blob segment %d: %w", i, err)
		}
		from, to := int64(0), plainLen
		if size > 0 {
			if start > plainStart {
				from = start - plainStart
			}
			if end+1 < plainStart+plainLen {
				to = end + 1 - plainStart
			}
		}
		count, err := writeAll(dst, plain[from:to])
		produced += count
		if err != nil {
			return produced, err
		}
	}
	expected := int64(0)
	if size > 0 {
		expected = end - start + 1
	}
	if produced != expected {
		return produced, errors.New("Blob did not decrypt to its recorded size.")
	}
	return produced, nil
}

func writeAll(dst io.Writer, p []byte) (int64, error) {
	var total int64
	for len(p) > 0 {
		n, err := dst.Write(p)
		total += int64(n)
		p = p[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
