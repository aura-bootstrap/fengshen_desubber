package tosstore

import (
	"context"
	"fmt"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"
)

type Store struct {
	client *tos.ClientV2
	bucket string
}

func New(endpoint, region, ak, sk, bucket string) (*Store, error) {
	cli, err := tos.NewClientV2(endpoint,
		tos.WithRegion(region),
		tos.WithCredentials(tos.NewStaticCredentials(ak, sk)))
	if err != nil {
		return nil, err
	}
	return &Store{client: cli, bucket: bucket}, nil
}

// UploadAndPresign 上传本地文件到 TOS 并返回预签名 GET URL。
func (s *Store) UploadAndPresign(ctx context.Context, localPath, key string, expiresSec int64) (string, error) {
	if _, err := s.client.PutObjectFromFile(ctx, &tos.PutObjectFromFileInput{
		PutObjectBasicInput: tos.PutObjectBasicInput{Bucket: s.bucket, Key: key},
		FilePath:            localPath,
	}); err != nil {
		return "", fmt.Errorf("tos put: %w", err)
	}
	out, err := s.client.PreSignedURL(&tos.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodGet,
		Bucket:     s.bucket,
		Key:        key,
		Expires:    expiresSec,
	})
	if err != nil {
		return "", fmt.Errorf("tos presign: %w", err)
	}
	return out.SignedUrl, nil
}
