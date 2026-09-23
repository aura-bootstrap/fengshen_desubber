package main

// Config is the dev-client parameter document edited in the Flutter config
// UI, snapshotted into every task, and copied as JSON for publishing to the
// admin client. Field defaults mirror `desub remove` CLI flag defaults.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aura-bootstrap/fengshen_desubber/internal/store"
)

type DetectConfig struct {
	BandStart      float64 `json:"band_start"`      // subtitle band start, fraction of frame height
	SceneThreshold float64 `json:"scene_threshold"` // scene cut threshold (0 disables)
	MaxGap         int     `json:"max_gap"`         // max gap frames inside one event
	CloseGap       int     `json:"close_gap"`       // temporal closing merge window (frames)
	CloseOverlap   float64 `json:"close_overlap"`   // temporal closing min overlap
	EdgePad        int     `json:"edge_pad"`        // frames padded before/after each event
	OCR            bool    `json:"ocr"`             // fuse easyocr sidecar detections
	OCRStride      int     `json:"ocr_stride"`      // OCR every Nth frame
	Alpha          bool    `json:"alpha"`           // unmix semi-transparent backdrop bars
}

type RepairConfig struct {
	Engine      string `json:"engine"`       // temporal | delogo
	Motion      bool   `json:"motion"`       // motion-compensated pixel transfer (temporal)
	Neighbors   int    `json:"neighbors"`    // temporal radius for pixel fill
	Pad         int    `json:"pad"`          // extra delogo box padding (px)
	Grain       bool   `json:"grain"`        // texture-match the repaired area
	ForceEngine string `json:"force_engine"` // "" | motion | propainter
}

type EnhanceConfig struct {
	ProPainter bool `json:"propainter"`
	// Painter 生成式旁车选择:""|propainter=默认 propainter_infer.py;
	// diffueraser / wanvace 换对应旁车脚本(权重路径走 DIFFUERASER_HOME /
	// WANVACE_HOME 环境变量,随引擎进程环境透传给子进程)。
	Painter          string `json:"painter"`
	PPMaskDilation   int    `json:"pp_mask_dilation"`
	PPTightDilate    int    `json:"pp_tight_dilate"`
	PPRaftIter       int    `json:"pp_raft_iter"`
	PPNeighborLength int    `json:"pp_neighbor_length"`
	PPConcurrency    int    `json:"pp_concurrency"`
	SAM2             bool   `json:"sam2"`
	FaceRestore      bool   `json:"face_restore"`
	VLMQC            bool   `json:"vlm_qc"`
}

type EncodeConfig struct {
	CRF    int    `json:"crf"`
	Preset string `json:"preset"`
}

type OutputConfig struct {
	Verify       bool    `json:"verify"`        // re-run detection on the output
	RiskCoverage float64 `json:"risk_coverage"` // high-risk coverage threshold
	Dir          string  `json:"dir"`           // 任务成功后复制产物到该目录
}

// OnlineConfig 云端去字幕:enabled 由创建/重跑的引擎选择写入任务快照(替代本地管线);
// server 为云端任务服务地址(登录缺省取此值,管理员账号/密码存 cloudauth.json 不进配置快照)。
type OnlineConfig struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
}

type Config struct {
	Detect  DetectConfig  `json:"detect"`
	Repair  RepairConfig  `json:"repair"`
	Enhance EnhanceConfig `json:"enhance"`
	Encode  EncodeConfig  `json:"encode"`
	Output  OutputConfig  `json:"output"`
	Online  OnlineConfig  `json:"online"`
}

func defaultConfig() *Config {
	return &Config{
		Detect: DetectConfig{
			BandStart: 0.58, SceneThreshold: 0.35, MaxGap: 3,
			CloseGap: 24, CloseOverlap: 0.5, EdgePad: 2,
			OCRStride: 12,
		},
		Repair: RepairConfig{
			Engine: "temporal", Motion: true, Neighbors: 6, Pad: 4,
		},
		Enhance: EnhanceConfig{
			PPMaskDilation: 8, PPTightDilate: 7, PPRaftIter: 32,
			PPNeighborLength: 20, PPConcurrency: 1,
		},
		Encode: EncodeConfig{CRF: 17, Preset: "medium"},
		Output: OutputConfig{Verify: true, RiskCoverage: 0.35},
	}
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := defaultConfig()
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

type fieldErr struct {
	Field string
	Msg   string
}

var x264Presets = map[string]bool{
	"ultrafast": true, "superfast": true, "veryfast": true, "faster": true,
	"fast": true, "medium": true, "slow": true, "slower": true, "veryslow": true,
}

func (c *Config) validate() []fieldErr {
	var errs []fieldErr
	bad := func(field, msg string) { errs = append(errs, fieldErr{field, msg}) }
	if c.Detect.BandStart < 0 || c.Detect.BandStart >= 1 {
		bad("detect.band_start", "须在 [0, 1) 区间")
	}
	if c.Detect.SceneThreshold < 0 || c.Detect.SceneThreshold > 1 {
		bad("detect.scene_threshold", "须在 [0, 1] 区间")
	}
	if c.Detect.MaxGap < 0 || c.Detect.MaxGap > 120 {
		bad("detect.max_gap", "须在 0..120")
	}
	if c.Detect.CloseGap < 0 || c.Detect.CloseGap > 600 {
		bad("detect.close_gap", "须在 0..600")
	}
	if c.Detect.CloseOverlap < 0 || c.Detect.CloseOverlap > 1 {
		bad("detect.close_overlap", "须在 [0, 1] 区间")
	}
	if c.Detect.EdgePad < 0 || c.Detect.EdgePad > 60 {
		bad("detect.edge_pad", "须在 0..60")
	}
	if c.Detect.OCRStride < 1 || c.Detect.OCRStride > 120 {
		bad("detect.ocr_stride", "须在 1..120")
	}
	switch c.Repair.Engine {
	case "temporal", "delogo":
	default:
		bad("repair.engine", "仅支持 temporal|delogo")
	}
	if c.Repair.Neighbors < 1 || c.Repair.Neighbors > 60 {
		bad("repair.neighbors", "须在 1..60")
	}
	if c.Repair.Pad < 0 || c.Repair.Pad > 64 {
		bad("repair.pad", "须在 0..64")
	}
	switch c.Repair.ForceEngine {
	case "", "motion", "propainter":
	default:
		bad("repair.force_engine", "仅支持 空|motion|propainter")
	}
	switch c.Enhance.Painter {
	case "", "propainter", "diffueraser", "wanvace":
	default:
		bad("enhance.painter", "仅支持 空|propainter|diffueraser|wanvace")
	}
	if c.Enhance.PPConcurrency < 1 || c.Enhance.PPConcurrency > 8 {
		bad("enhance.pp_concurrency", "须在 1..8")
	}
	if c.Encode.CRF < 0 || c.Encode.CRF > 51 {
		bad("encode.crf", "须在 0..51")
	}
	if !x264Presets[c.Encode.Preset] {
		bad("encode.preset", "未知 x264 preset")
	}
	if c.Output.RiskCoverage < 0 || c.Output.RiskCoverage > 1 {
		bad("output.risk_coverage", "须在 [0, 1] 区间")
	}
	if s := strings.TrimSpace(c.Online.Server); s != "" &&
		!strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		bad("online.server", "须为 http(s):// 地址或留空")
	}
	return errs
}

func (s *server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadConfig(s.cfgPath)
	if err != nil {
		cfg = defaultConfig()
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handlePutConfig merges the submitted object onto the current config
// (unknown fields rejected), validates, saves, and records history.
func (s *server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "请求体读取失败")
		return
	}
	cfg, err := loadConfig(s.cfgPath)
	if err != nil {
		cfg = defaultConfig()
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
		return
	}
	if errs := cfg.validate(); len(errs) > 0 {
		msgs := make([]map[string]string, len(errs))
		for i, e := range errs {
			msgs[i] = map[string]string{"field": e.Field, "msg": e.Msg}
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": msgs})
		return
	}
	if err := cfg.save(s.cfgPath); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	if s.taskDB != nil {
		if pb, merr := json.Marshal(cfg); merr == nil {
			if _, herr := s.taskDB.AddConfigHistory(string(pb)); herr != nil {
				fmt.Println("配置历史写入失败:", herr)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *server) handleConfigHistory(w http.ResponseWriter, r *http.Request) {
	if s.taskDB == nil {
		writeJSON(w, http.StatusOK, []store.ConfigHistEntry{})
		return
	}
	list, err := s.taskDB.ListConfigHistory()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "配置历史读取失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) handleConfigHistoryGet(w http.ResponseWriter, r *http.Request) {
	v, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || v < 1 {
		writeErr(w, http.StatusBadRequest, "版本号非法")
		return
	}
	if s.taskDB == nil {
		writeErr(w, http.StatusNotFound, "历史版本不存在")
		return
	}
	payload, ok, err := s.taskDB.GetConfigHistory(v)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "配置历史读取失败: "+err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "历史版本不存在")
		return
	}
	var historical Config
	if err := json.Unmarshal([]byte(payload), &historical); err != nil {
		writeErr(w, http.StatusInternalServerError, "历史配置解析失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, &historical)
}
