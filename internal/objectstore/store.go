package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"photodrop/internal/config"
)

var ErrObjectNotFound = errors.New("object not found")

type Store struct {
	client     *minio.Client
	signer     *minio.Client
	core       minio.Core
	bucket     string
	presignTTL time.Duration
}

type ObjectInfo struct {
	Size        int64
	ContentType string
	ETag        string
}

type CompletedPart struct {
	PartNumber int
	ETag       string
}

func New(cfg config.S3Config) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	signer, err := minio.New(cfg.PublicEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.PublicUseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create public S3 signer: %w", err)
	}
	return &Store{
		client:     client,
		signer:     signer,
		core:       minio.Core{Client: client},
		bucket:     cfg.Bucket,
		presignTTL: cfg.PresignTTL,
	}, nil
}

func (s *Store) Ready(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("bucket %q does not exist", s.bucket)
	}
	return nil
}

func (s *Store) RemovePrefix(ctx context.Context, prefix string) error {
	objects := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	remove := make(chan minio.ObjectInfo)
	go func() {
		defer close(remove)
		for object := range objects {
			if object.Err != nil {
				remove <- object
				return
			}
			select {
			case remove <- object:
			case <-ctx.Done():
				return
			}
		}
	}()

	for result := range s.client.RemoveObjects(ctx, s.bucket, remove, minio.RemoveObjectsOptions{}) {
		if result.Err != nil {
			return fmt.Errorf("remove object %q: %w", result.ObjectName, result.Err)
		}
	}
	return nil
}

func (s *Store) PresignPut(ctx context.Context, objectKey, contentType string, sessionExpiry time.Time) (string, time.Time, error) {
	ttl, expiresAt, err := s.effectivePresignTTL(sessionExpiry)
	if err != nil {
		return "", time.Time{}, err
	}
	presigned, err := s.signer.PresignHeader(
		ctx,
		http.MethodPut,
		s.bucket,
		objectKey,
		ttl,
		nil,
		http.Header{"Content-Type": []string{contentType}},
	)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign object PUT: %w", err)
	}
	return presigned.String(), expiresAt, nil
}

func (s *Store) PresignGet(ctx context.Context, objectKey, downloadName string) (string, time.Time, error) {
	parameters := url.Values{}
	if downloadName != "" {
		parameters.Set("response-content-disposition", contentDisposition(downloadName))
	}
	presigned, err := s.signer.PresignedGetObject(ctx, s.bucket, objectKey, s.presignTTL, parameters)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign object GET: %w", err)
	}
	return presigned.String(), time.Now().Add(s.presignTTL).UTC(), nil
}

func (s *Store) PresignInternalGet(ctx context.Context, objectKey string, ttl time.Duration) (string, error) {
	presigned, err := s.client.PresignedGetObject(ctx, s.bucket, objectKey, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("presign internal object GET: %w", err)
	}
	return presigned.String(), nil
}

func (s *Store) PutFile(ctx context.Context, objectKey, filename, contentType string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open upload file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat upload file: %w", err)
	}
	if _, err := s.client.PutObject(ctx, s.bucket, objectKey, file, info.Size(), minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return fmt.Errorf("put object file: %w", err)
	}
	return nil
}

func (s *Store) PutReader(ctx context.Context, objectKey string, reader io.Reader, contentType string) (int64, error) {
	info, err := s.client.PutObject(ctx, s.bucket, objectKey, reader, -1, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return 0, fmt.Errorf("put object stream: %w", err)
	}
	return info.Size, nil
}

func (s *Store) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, fmt.Errorf("stat opened object: %w", err)
	}
	return object, nil
}

func (s *Store) StartMultipart(ctx context.Context, objectKey, contentType string) (string, error) {
	uploadID, err := s.core.NewMultipartUpload(ctx, s.bucket, objectKey, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("start multipart upload: %w", err)
	}
	return uploadID, nil
}

func (s *Store) PresignPart(ctx context.Context, objectKey, uploadID string, partNumber int, sessionExpiry time.Time) (string, time.Time, error) {
	ttl, expiresAt, err := s.effectivePresignTTL(sessionExpiry)
	if err != nil {
		return "", time.Time{}, err
	}
	parameters := url.Values{
		"partNumber": []string{strconv.Itoa(partNumber)},
		"uploadId":   []string{uploadID},
	}
	presigned, err := s.signer.Presign(ctx, http.MethodPut, s.bucket, objectKey, ttl, parameters)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign multipart part: %w", err)
	}
	return presigned.String(), expiresAt, nil
}

func (s *Store) CompleteMultipart(ctx context.Context, objectKey, uploadID, contentType string, parts []CompletedPart) error {
	sort.Slice(parts, func(left, right int) bool { return parts[left].PartNumber < parts[right].PartNumber })
	completed := make([]minio.CompletePart, len(parts))
	for index, part := range parts {
		completed[index] = minio.CompletePart{PartNumber: part.PartNumber, ETag: part.ETag}
	}
	if _, err := s.core.CompleteMultipartUpload(
		ctx,
		s.bucket,
		objectKey,
		uploadID,
		completed,
		minio.PutObjectOptions{ContentType: contentType},
	); err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	return nil
}

func (s *Store) AbortMultipart(ctx context.Context, objectKey, uploadID string) error {
	if err := s.core.AbortMultipartUpload(ctx, s.bucket, objectKey, uploadID); err != nil {
		return fmt.Errorf("abort multipart upload: %w", err)
	}
	return nil
}

func (s *Store) Stat(ctx context.Context, objectKey string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.Code == "NoSuchKey" || response.Code == "NoSuchObject" || response.StatusCode == http.StatusNotFound {
			return ObjectInfo{}, ErrObjectNotFound
		}
		return ObjectInfo{}, fmt.Errorf("stat object: %w", err)
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType, ETag: info.ETag}, nil
}

func (s *Store) Remove(ctx context.Context, objectKey string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object: %w", err)
	}
	return nil
}

func (s *Store) effectivePresignTTL(sessionExpiry time.Time) (time.Duration, time.Time, error) {
	ttl := s.presignTTL
	remaining := time.Until(sessionExpiry)
	if remaining < ttl {
		ttl = remaining
	}
	if ttl < time.Second {
		return 0, time.Time{}, fmt.Errorf("upload session has expired")
	}
	return ttl, time.Now().Add(ttl).UTC(), nil
}

func contentDisposition(filename string) string {
	var fallback strings.Builder
	for _, character := range filename {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._- ", character) {
			fallback.WriteRune(character)
		} else {
			fallback.WriteByte('_')
		}
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fallback.String(), url.PathEscape(filename))
}
