package externalstorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/outbound"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3-compatible storage as folders: "/" in a key separates folders and an empty
// folder is a zero-byte "name/" object. Everything lives below the optional
// prefix. Renames are copy + delete, as S3 has no rename.

const (
	// Folder moves and deletes are bounded.
	maxTreeObjects = 5000
	copyLimit      = 5 << 30
)

type s3Adapter struct {
	client   *s3.Client
	http     *http.Client
	bucket   string
	basePath string
}

func newS3(config settings, guard outbound.Guard) *s3Adapter {
	endpoint, region := config.text("endpoint"), config.text("region")
	if region == "" {
		region = "us-east-1"
	}
	httpClient := guard.HTTPClient()
	client := s3.NewFromConfig(aws.Config{
		Region:           region,
		Credentials:      credentials.NewStaticCredentialsProvider(config.text("accessKeyId"), config.text("secretAccessKey"), ""),
		HTTPClient:       httpClient,
		RetryMaxAttempts: 2,
	}, func(o *s3.Options) {
		o.UsePathStyle = config.flag("forcePathStyle") || endpoint != ""
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	base := strings.Join(strings.FieldsFunc(config.text("prefix"), func(r rune) bool { return r == '/' }), "/")
	return &s3Adapter{client: client, http: httpClient, bucket: config.text("bucket"), basePath: base}
}

func (a *s3Adapter) key(path string) string {
	switch {
	case a.basePath == "":
		return path
	case path == "":
		return a.basePath
	}
	return a.basePath + "/" + path
}

func (a *s3Adapter) folderPrefix(path string) string {
	if key := a.key(path); key != "" {
		return key + "/"
	}
	return ""
}

func s3Status(err error) int {
	var response *awshttp.ResponseError
	if errors.As(err, &response) {
		return response.HTTPStatusCode()
	}
	return 0
}

func s3Code(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}

func s3Missing(err error) bool {
	code := s3Code(err)
	return code == "NotFound" || code == "NoSuchKey" || s3Status(err) == http.StatusNotFound
}

func s3Error(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, outbound.ErrBlocked):
		return hostBlocked()
	case s3Status(err) == http.StatusForbidden:
		return apperr.New(http.StatusBadGateway, "The provider denied access. Check the keys and permissions.", "EXTERNAL_FORBIDDEN")
	case s3Code(err) == "NoSuchBucket":
		return apperr.New(http.StatusBadGateway, "That bucket does not exist.", "EXTERNAL_NOT_FOUND")
	case s3Missing(err):
		return notFound()
	}
	return apperr.New(http.StatusBadGateway, "The provider could not be reached or refused the request.", "EXTERNAL_UNREACHABLE")
}

func (a *s3Adapter) page(ctx context.Context, prefix, delimiter string, token *string, size int32) (*s3.ListObjectsV2Output, error) {
	input := &s3.ListObjectsV2Input{Bucket: &a.bucket, Prefix: &prefix, ContinuationToken: token}
	if delimiter != "" {
		input.Delimiter = &delimiter
	}
	if size > 0 {
		input.MaxKeys = &size
	}
	page, err := a.client.ListObjectsV2(ctx, input)
	return page, s3Error(err)
}

func (a *s3Adapter) open(ctx context.Context) error {
	_, err := a.page(ctx, a.folderPrefix(""), "", nil, 1)
	return err
}

func (a *s3Adapter) close() { a.http.CloseIdleConnections() }

func (a *s3Adapter) list(ctx context.Context, path string, limit int) ([]entry, bool, error) {
	prefix := a.folderPrefix(path)
	entries := []entry{}
	var token *string
	for {
		page, err := a.page(ctx, prefix, "/", token, 0)
		if err != nil {
			return nil, false, err
		}
		for _, folder := range page.CommonPrefixes {
			name := baseName(strings.TrimSuffix(aws.ToString(folder.Prefix), "/"))
			if safeListedName(name) {
				entries = append(entries, entry{name: name, folder: true})
			}
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			name := baseName(key)
			if !strings.HasSuffix(key, "/") && safeListedName(name) {
				entries = append(entries, entry{name: name, size: aws.ToInt64(object.Size), modified: aws.ToTime(object.LastModified)})
			}
		}
		token = nil
		if aws.ToBool(page.IsTruncated) {
			token = page.NextContinuationToken
		}
		if len(entries) >= limit {
			return entries[:limit], token != nil || len(entries) > limit, nil
		}
		if token == nil {
			return entries, false, nil
		}
	}
}

func (a *s3Adapter) stat(ctx context.Context, path string) (*entry, error) {
	if path == "" {
		return &entry{folder: true}, nil
	}
	key := a.key(path)
	head, err := a.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &a.bucket, Key: &key})
	if err == nil {
		return &entry{name: baseName(path), size: aws.ToInt64(head.ContentLength), modified: aws.ToTime(head.LastModified)}, nil
	}
	if !s3Missing(err) {
		return nil, s3Error(err)
	}
	page, err := a.page(ctx, a.folderPrefix(path), "", nil, 1)
	if err != nil || len(page.Contents) == 0 {
		return nil, err
	}
	return &entry{name: baseName(path), folder: true}, nil
}

func (a *s3Adapter) read(ctx context.Context, path string, part *byteRange) (io.ReadCloser, error) {
	key := a.key(path)
	input := &s3.GetObjectInput{Bucket: &a.bucket, Key: &key}
	if part != nil {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", part.start, part.end))
	}
	output, err := a.client.GetObject(ctx, input)
	if err != nil {
		return nil, s3Error(err)
	}
	return output.Body, nil
}

func (a *s3Adapter) write(ctx context.Context, path string, body io.Reader, size int64) error {
	key := a.key(path)
	input := &transfermanager.UploadObjectInput{Bucket: &a.bucket, Key: &key, Body: body}
	if size >= 0 {
		input.ContentLength = &size
	}
	uploader := transfermanager.New(a.client, func(o *transfermanager.Options) { o.PartSizeBytes = 16 << 20; o.Concurrency = 2 })
	_, err := uploader.UploadObject(ctx, input)
	return s3Error(err)
}

func (a *s3Adapter) mkdir(ctx context.Context, path string) error {
	key := a.folderPrefix(path)
	_, err := a.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &a.bucket, Key: &key, Body: strings.NewReader("")})
	return s3Error(err)
}

// keysBelow is every object of a folder, its marker included.
func (a *s3Adapter) keysBelow(ctx context.Context, path string) ([]string, error) {
	var keys []string
	var token *string
	for {
		page, err := a.page(ctx, a.folderPrefix(path), "", token, 0)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
		if len(keys) > maxTreeObjects {
			return nil, apperr.New(http.StatusRequestEntityTooLarge, "This folder holds too many items to change in one go.", "EXTERNAL_TOO_LARGE")
		}
		if !aws.ToBool(page.IsTruncated) {
			return keys, nil
		}
		token = page.NextContinuationToken
	}
}

func (a *s3Adapter) removeKeys(ctx context.Context, keys []string) error {
	for start := 0; start < len(keys); start += 1000 {
		batch := keys[start:min(start+1000, len(keys))]
		objects := make([]types.ObjectIdentifier, len(batch))
		for i := range batch {
			objects[i] = types.ObjectIdentifier{Key: &batch[i]}
		}
		output, err := a.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: &a.bucket, Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)}})
		if err != nil {
			return s3Error(err)
		}
		// S3 answers success while refusing single keys.
		if len(output.Errors) > 0 {
			return apperr.New(http.StatusBadGateway, "The provider could not remove every item.", "EXTERNAL_ERROR")
		}
	}
	return nil
}

func (a *s3Adapter) copyKey(ctx context.Context, from, to string) error {
	parts := strings.Split(a.bucket+"/"+from, "/")
	for i := range parts {
		parts[i] = url.QueryEscape(parts[i])
		parts[i] = strings.ReplaceAll(parts[i], "+", "%20")
	}
	_, err := a.client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: &a.bucket, Key: &to, CopySource: aws.String(strings.Join(parts, "/"))})
	return s3Error(err)
}

func (a *s3Adapter) move(ctx context.Context, from, to string) error {
	info, err := a.stat(ctx, from)
	if err != nil {
		return err
	}
	if info == nil {
		return notFound()
	}
	if !info.folder {
		if info.size > copyLimit {
			return apperr.New(http.StatusRequestEntityTooLarge, "This file is too large to rename on this provider.", "EXTERNAL_TOO_LARGE")
		}
		if err = a.copyKey(ctx, a.key(from), a.key(to)); err != nil {
			return err
		}
		return a.removeKeys(ctx, []string{a.key(from)})
	}
	keys, err := a.keysBelow(ctx, from)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err = a.copyKey(ctx, key, a.folderPrefix(to)+strings.TrimPrefix(key, a.folderPrefix(from))); err != nil {
			return err
		}
	}
	return a.removeKeys(ctx, keys)
}

func (a *s3Adapter) remove(ctx context.Context, path string) error {
	info, err := a.stat(ctx, path)
	if err != nil {
		return err
	}
	if info == nil {
		return notFound()
	}
	keys := []string{a.key(path)}
	if info.folder {
		if keys, err = a.keysBelow(ctx, path); err != nil {
			return err
		}
	}
	return a.removeKeys(ctx, keys)
}
