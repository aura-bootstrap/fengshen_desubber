package ddbstore

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// scanKeys 扫描全部带 meta 的键，翻页取尽，按前缀过滤返回逻辑键名（含前缀）。
func (s *Store) scanKeys(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	var start map[string]types.AttributeValue
	for pages := 0; pages < 1000; pages++ {
		pks, last, err := s.cli.WithContext(ctx).ScanMetaKeys(0, start, s.Now().Unix())
		if err != nil {
			return nil, err
		}
		for _, pk := range pks {
			if strings.HasPrefix(pk, prefix) {
				out = append(out, pk)
			}
		}
		if len(last) == 0 {
			return out, nil
		}
		start = last
	}
	return out, nil
}
