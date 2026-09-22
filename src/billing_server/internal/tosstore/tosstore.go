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

// PresignPut 预签名 PUT URL:客户端直传原片到 key,过期时间 expiresSec 秒。
func (s *Store) PresignPut(ctx context.Context, key string, expiresSec int64) (string, error) {
	out, err := s.client.PreSignedURL(&tos.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodPut,
		Bucket:     s.bucket,
		Key:        key,
		Expires:    expiresSec,
	})
	if err != nil {
		return "", fmt.Errorf("tos presign put: %w", err)
	}
	return out.SignedUrl, nil
}

// PresignGet 预签名 GET URL:算子回源/服务端探测时长用。
func (s *Store) PresignGet(ctx context.Context, key string, expiresSec int64) (string, error) {
	out, err := s.client.PreSignedURL(&tos.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodGet,
		Bucket:     s.bucket,
		Key:        key,
		Expires:    expiresSec,
	})
	if err != nil {
		return "", fmt.Errorf("tos presign get: %w", err)
	}
	return out.SignedUrl, nil
}

// Delete 删除 TOS 对象(任务终态后清理输入视频等临时对象)。
func (s *Store) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObjectV2(ctx, &tos.DeleteObjectV2Input{
		Bucket: s.bucket, Key: key,
	}); err != nil {
		return fmt.Errorf("tos delete: %w", err)
	}
	return nil
}

// EnsureInputLifecycle 幂等设置桶生命周期:input/ 前缀 days 天后自动删除。
// 兜底清理「客户端上传失败/建单后从未提交」的孤儿临时视频(正常终态由 Delete 即时清理)。
func (s *Store) EnsureInputLifecycle(ctx context.Context, days int64) error {
	rule := tos.LifecycleRule{
		ID:         "expire-input",
		Prefix:     "input/",
		Status:     enum.StatusEnabled,
		Expiration: &tos.Expiration{Days: int(days)},
	}
	// 读现有规则:同名规则覆盖、其余保留(生命周期按桶整体覆盖写)。
	var rules []tos.LifecycleRule
	if out, err := s.client.GetBucketLifecycle(ctx, &tos.GetBucketLifecycleInput{Bucket: s.bucket}); err == nil {
		for _, r := range out.Rules {
			if r.ID != rule.ID {
				rules = append(rules, r)
			}
		}
	}
	rules = append(rules, rule)
	if _, err := s.client.PutBucketLifecycle(ctx, &tos.PutBucketLifecycleInput{
		Bucket: s.bucket, Rules: rules,
	}); err != nil {
		return fmt.Errorf("tos put lifecycle: %w", err)
	}
	return nil
}
