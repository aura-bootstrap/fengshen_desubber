// Package provider 多平台扩展层：对象存储上传与去字幕算子的抽象 + 注册表。
// 当前仅接入火山 LAS 一家（cmd/server 注册为 "las" 并设为默认），
// 后续新增平台只需实现 Uploader/Operator 并 Register 进 Registry。
package provider

import "context"

// Uploader 对象存储上传抽象：上传本地文件并返回预签名 GET URL。
type Uploader interface {
	UploadAndPresign(ctx context.Context, localPath, key string, expiresSec int64) (string, error)
}

// Operator 去字幕算子抽象：提交/轮询/下载三段式。
type Operator interface {
	Submit(ctx context.Context, videoURL, clientToken string) (string, error)
	Poll(ctx context.Context, taskID string) (status, videoURL, errMsg string, err error)
	Download(ctx context.Context, url, dstPath string) error
}

// Provider 一套平台能力：名字 + 上传 + 算子。
type Provider struct {
	Name     string
	Uploader Uploader
	Operator Operator
}

// Registry 平台注册表：按名查找，缺省平台兜底。
type Registry struct {
	providers map[string]Provider
	def       string
}

// NewRegistry 建注册表，def 为默认平台名（任务未指定平台时使用）。
func NewRegistry(def string) *Registry {
	return &Registry{providers: make(map[string]Provider), def: def}
}

// Register 注册平台，同名覆盖。
func (r *Registry) Register(p Provider) {
	r.providers[p.Name] = p
}

// Get 按名取平台，不存在返回 ok=false。
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Default 返回默认平台（未注册时返回零值，调用方应保证已注册）。
func (r *Registry) Default() Provider {
	return r.providers[r.def]
}

// DefaultName 返回默认平台名。
func (r *Registry) DefaultName() string {
	return r.def
}
