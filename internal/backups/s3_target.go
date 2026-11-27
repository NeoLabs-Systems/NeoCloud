package backups

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	transfertypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3Config describes an AWS S3 or S3-compatible backup target.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	Prefix          string
	ForcePathStyle  bool
	StorageClass    string
}

// S3Target stores encrypted snapshots and blobs in a configured bucket.
// Archived storage classes can be written, listed, and hydrated on demand.
type S3Target struct {
	client       *s3.Client
	uploader     *transfermanager.Client
	httpClient   *http.Client
	bucket       string
	prefix       string
	storageClass types.StorageClass
}

type S3Object struct {
	Key          string
	Size         int64
	StorageClass string
}

const (
	s3MaxSingleCopy = int64(5 << 30)
	s3CopyPartSize  = int64(512 << 20)
	s3MaxCopyParts  = int64(10000)
)

func NewS3Target(config S3Config) (*S3Target, error) {
	return NewS3TargetWithHTTPClient(config, ssrf.NewClient(ssrf.Options{AllowPrivate: true, Timeout: 2 * time.Minute}))
}

// NewS3TargetWithHTTPClient allows protocol fixtures to provide a local
// transport. Production callers should use NewS3Target's SSRF-aware client.
func NewS3TargetWithHTTPClient(config S3Config, clientHTTP *http.Client) (*S3Target, error) {
	if strings.TrimSpace(config.Bucket) == "" || strings.TrimSpace(config.AccessKeyID) == "" || strings.TrimSpace(config.SecretAccessKey) == "" {
		return nil, errors.New("S3 bucket, access key, and secret key are required")
	}
	if clientHTTP == nil {
		return nil, errors.New("S3 HTTP client is required")
	}
	if config.Region == "" {
		config.Region = "us-east-1"
	}
	if config.StorageClass == "" {
		config.StorageClass = "STANDARD"
	}
	storageClass := types.StorageClass(config.StorageClass)
	switch storageClass {
	case types.StorageClassStandard, types.StorageClassGlacier, types.StorageClassDeepArchive, types.StorageClassGlacierIr, types.StorageClassStandardIa:
	default:
		return nil, fmt.Errorf("unsupported S3 storage class %q", config.StorageClass)
	}
	if config.Endpoint != "" {
		if err := validateS3Endpoint(config.Endpoint); err != nil {
			return nil, err
		}
	}
	cfg := aws.Config{
		Region:      config.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, "")),
		HTTPClient:  clientHTTP,
	}
	options := []func(*s3.Options){func(o *s3.Options) { o.UsePathStyle = config.ForcePathStyle }}
	if config.Endpoint != "" {
		options = append(options, func(o *s3.Options) { o.BaseEndpoint = aws.String(config.Endpoint) })
	}
	client := s3.NewFromConfig(cfg, options...)
	prefix := strings.Trim(config.Prefix, "/")
	return &S3Target{client: client, uploader: transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = 16 << 20
		o.Concurrency = 2
	}), httpClient: clientHTTP, bucket: config.Bucket, prefix: prefix, storageClass: storageClass}, nil
}

func validateS3Endpoint(raw string) error {
	endpoint, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil || endpoint.URL.Host == "" || (endpoint.URL.Scheme != "https" && endpoint.URL.Scheme != "http") || endpoint.URL.User != nil || endpoint.URL.RawQuery != "" || endpoint.URL.Fragment != "" {
		return errors.New("S3 endpoint must be an HTTP or HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

func (t *S3Target) key(key string) (string, error) {
	if err := validTargetKey(key, false); err != nil {
		return "", err
	}
	if t.prefix == "" {
		return key, nil
	}
	return t.prefix + "/" + key, nil
}

func (t *S3Target) relative(key string) (string, bool) {
	if t.prefix == "" {
		return key, true
	}
	prefix := t.prefix + "/"
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	return strings.TrimPrefix(key, prefix), true
}

func (t *S3Target) Open(ctx context.Context) error {
	_, err := t.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(t.bucket)})
	return err
}

func (t *S3Target) Close() error {
	t.httpClient.CloseIdleConnections()
	return nil
}

func (t *S3Target) Exists(ctx context.Context, key string) (bool, error) {
	full, err := t.key(key)
	if err != nil {
		return false, err
	}
	_, err = t.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(full)})
	if err == nil {
		return true, nil
	}
	if s3ObjectNotFound(err) {
		return false, nil
	}
	return false, err
}

func (t *S3Target) StatObject(ctx context.Context, key string) (archiveObjectStat, error) {
	full, err := t.key(key)
	if err != nil {
		return archiveObjectStat{}, err
	}
	output, err := t.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(full)})
	if err != nil {
		if s3ObjectNotFound(err) {
			return archiveObjectStat{}, nil
		}
		return archiveObjectStat{}, err
	}
	storageClass := string(output.StorageClass)
	if storageClass == "" {
		storageClass = string(types.StorageClassStandard)
	}
	state := ""
	if output.Restore != nil {
		header := aws.ToString(output.Restore)
		if strings.Contains(header, `ongoing-request="true"`) {
			state = "ongoing"
		} else if header != "" {
			state = "available"
		}
	}
	return archiveObjectStat{Exists: true, StorageClass: storageClass, Restore: state}, nil
}

func (t *S3Target) RequestObjectRestore(ctx context.Context, key, tier string) error {
	full, err := t.key(key)
	if err != nil {
		return err
	}
	var restoreTier types.Tier
	switch tier {
	case "Standard":
		restoreTier = types.TierStandard
	case "Bulk":
		restoreTier = types.TierBulk
	default:
		return errors.New("restore tier must be Standard or Bulk")
	}
	days := int32(7)
	_, err = t.client.RestoreObject(ctx, &s3.RestoreObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(full), RestoreRequest: &types.RestoreRequest{Days: &days, GlacierJobParameters: &types.GlacierJobParameters{Tier: restoreTier}}})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && api.ErrorCode() == "RestoreAlreadyInProgress" {
			return nil
		}
		return err
	}
	return nil
}

func s3ObjectNotFound(err error) bool {
	var api smithy.APIError
	if errors.As(err, &api) {
		code := strings.ToLower(api.ErrorCode())
		if code == "nosuchkey" || code == "notfound" || code == "nosuchobject" {
			return true
		}
	}
	var response *smithyhttp.ResponseError
	return errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotFound
}

func (t *S3Target) List(ctx context.Context, prefix string) ([]string, error) {
	objects, err := t.ListObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(objects))
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	return keys, nil
}

func (t *S3Target) ListObjects(ctx context.Context, prefix string) ([]S3Object, error) {
	trailingSlash := strings.HasSuffix(prefix, "/")
	cleanPrefix := strings.TrimSuffix(prefix, "/")
	full, err := t.key(cleanPrefix)
	if err != nil {
		return nil, err
	}
	if trailingSlash {
		full += "/"
	}
	objects := make([]S3Object, 0)
	var token *string
	for {
		page, listErr := t.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(t.bucket), Prefix: aws.String(full), ContinuationToken: token, MaxKeys: aws.Int32(1000),
		})
		if listErr != nil {
			return nil, listErr
		}
		for _, object := range page.Contents {
			key, ok := t.relative(aws.ToString(object.Key))
			if ok {
				storageClass := string(object.StorageClass)
				if storageClass == "" {
					storageClass = string(types.StorageClassStandard)
				}
				objects = append(objects, S3Object{Key: key, Size: aws.ToInt64(object.Size), StorageClass: storageClass})
			}
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		if page.NextContinuationToken == nil || aws.ToString(page.NextContinuationToken) == "" {
			return nil, errors.New("S3 listing was truncated without a continuation token")
		}
		token = page.NextContinuationToken
	}
	return objects, nil
}

// ChangeStorageClass rewrites an object in place without deleting it. S3's
// single-copy API is limited to 5 GiB, so larger backups use multipart copy.
func (t *S3Target) ChangeStorageClass(ctx context.Context, key string, size int64) error {
	full, err := t.key(key)
	if err != nil {
		return err
	}
	copySource := s3CopySource(t.bucket, full)
	if size <= s3MaxSingleCopy {
		_, err = t.client.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket: aws.String(t.bucket), Key: aws.String(full), CopySource: aws.String(copySource),
			StorageClass: t.storageClass, MetadataDirective: types.MetadataDirectiveCopy,
		})
		return err
	}
	partSize := s3CopyPartSize
	if minimum := (size + s3MaxCopyParts - 1) / s3MaxCopyParts; minimum > partSize {
		partSize = minimum
	}
	created, err := t.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(t.bucket), Key: aws.String(full), StorageClass: t.storageClass,
	})
	if err != nil {
		return err
	}
	if created.UploadId == nil || aws.ToString(created.UploadId) == "" {
		return errors.New("S3 did not return a multipart upload id")
	}
	completed := false
	defer func() {
		if !completed {
			_, _ = t.client.AbortMultipartUpload(context.Background(), &s3.AbortMultipartUploadInput{
				Bucket: aws.String(t.bucket), Key: aws.String(full), UploadId: created.UploadId,
			})
		}
	}()
	parts := make([]types.CompletedPart, 0, (size+partSize-1)/partSize)
	for offset, partNumber := int64(0), int32(1); offset < size; partNumber++ {
		end := offset + partSize - 1
		if end >= size {
			end = size - 1
		}
		result, copyErr := t.client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
			Bucket: aws.String(t.bucket), Key: aws.String(full), UploadId: created.UploadId,
			PartNumber: aws.Int32(partNumber), CopySource: aws.String(copySource),
			CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", offset, end)),
		})
		if copyErr != nil {
			return copyErr
		}
		var etag *string
		if result.CopyPartResult != nil {
			etag = result.CopyPartResult.ETag
		}
		if etag == nil || aws.ToString(etag) == "" {
			return errors.New("S3 did not return a copied part etag")
		}
		parts = append(parts, types.CompletedPart{ETag: etag, PartNumber: aws.Int32(partNumber)})
		offset = end + 1
	}
	_, err = t.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(t.bucket), Key: aws.String(full), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return err
	}
	completed = true
	return nil
}

func s3CopySource(bucket, key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return bucket + "/" + strings.Join(parts, "/")
}

func (t *S3Target) Put(ctx context.Context, key, source string) error {
	full, err := t.key(key)
	if err != nil {
		return err
	}
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	_, err = t.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(t.bucket), Key: aws.String(full), Body: file,
		ContentLength: aws.Int64(info.Size()), StorageClass: transfertypes.StorageClass(t.storageClass),
	})
	return err
}

func (t *S3Target) Get(ctx context.Context, key, destination string) error {
	full, err := t.key(key)
	if err != nil {
		return err
	}
	result, err := t.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(full)})
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".neocloud-s3-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = io.Copy(tmp, result.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, 0600); err != nil {
		return err
	}
	return os.Rename(tmpName, destination)
}

func (t *S3Target) Delete(ctx context.Context, key string) error {
	full, err := t.key(key)
	if err != nil {
		return err
	}
	_, err = t.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(full)})
	return err
}

func (t *S3Target) DeletePrefix(ctx context.Context, prefix string) error {
	full, err := t.key(prefix)
	if err != nil {
		return err
	}
	var token *string
	for {
		page, listErr := t.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(t.bucket), Prefix: aws.String(full), ContinuationToken: token, MaxKeys: aws.Int32(1000),
		})
		if listErr != nil {
			return listErr
		}
		if len(page.Contents) > 0 {
			objects := make([]types.ObjectIdentifier, 0, len(page.Contents))
			for _, object := range page.Contents {
				objects = append(objects, types.ObjectIdentifier{Key: object.Key})
			}
			deleted, deleteErr := t.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(t.bucket), Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
			})
			if deleteErr != nil {
				return deleteErr
			}
			if len(deleted.Errors) > 0 {
				return fmt.Errorf("S3 could not delete every object under the requested prefix")
			}
		}
		if !aws.ToBool(page.IsTruncated) {
			return nil
		}
		if page.NextContinuationToken == nil || aws.ToString(page.NextContinuationToken) == "" {
			return errors.New("S3 listing was truncated without a continuation token")
		}
		token = page.NextContinuationToken
	}
}
