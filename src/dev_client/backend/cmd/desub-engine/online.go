package main

// 在线去字幕执行链路(任务快照 online.enabled=true 时替代本地 desub 管线):
// 读 cardkey.json -> 建单拿预签名 URL 直传原片到 TOS -> submit 提交算子 ->
// 每 15s 轮询状态(上限 6 小时,长视频) -> completed 后下载成片(302 到算子侧地址)
// 到本任务约定的输出位置(workDir/outName)。
// 移植自用户版 internal/runner/online.go;平台选择透传空值走服务端默认平台。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aura-bootstrap/fengshen_desubber/internal/billing"
	"github.com/aura-bootstrap/fengshen_desubber/internal/cardkey"
)

const (
	onlinePollInterval  = 15 * time.Second // 云端状态轮询间隔
	onlinePollTimeout   = 6 * time.Hour    // 长视频轮询上限
	xferProgressMinStep = time.Second      // 上传/下载进度事件最小间隔(节流)
)

// xferProgress 把 billing 的字节回调节流转成 SSE progress 事件(Done/Total 单位:字节)。
func xferProgress(stage string, events chan<- Event) billing.ProgressFn {
	var last time.Time
	return func(done, total int64) {
		now := time.Now()
		if now.Sub(last) < xferProgressMinStep && done < total {
			return
		}
		last = now
		events <- Event{Type: "progress", Stage: stage, Done: int(done), Total: int(total)}
	}
}

// runOnline 在线去字幕主流程。ctx 取消(用户停止任务)会中断上传/轮询/下载。
func runOnline(ctx context.Context, keyDir, workDir, srcPath, outName string, events chan<- Event) error {
	kf, err := cardkey.LoadKeyFile(keyDir)
	if err != nil {
		if errors.Is(err, cardkey.ErrNotActivated) {
			return errors.New("在线去字幕需要先激活卡密(新建任务选在线引擎后激活)")
		}
		return fmt.Errorf("读取本地卡密信息失败: %v", err)
	}
	cli := billing.New(kf.Server, kf.CardKey, kf.MachineHash)

	// 1. 上传原片建单。
	events <- Event{Type: "stage", Stage: "upload"}
	events <- Event{Type: "log", Msg: "上传视频到云端去字幕服务..."}
	created, err := cli.CreateTask(ctx, srcPath, "", xferProgress("upload", events))
	if err != nil {
		return fmt.Errorf("云端建单失败: %s", billing.Message(err))
	}
	events <- Event{Type: "log", Msg: fmt.Sprintf(
		"云端任务 %s 已创建(时长 %.1fs,扣点 %d,余额 %d)",
		created.TaskID, created.DurationSec, created.Cost, created.Balance)}
	// 顺带刷新本地缓存余额(status 接口远端不可达时兜底显示)。
	kf.Credits = created.Balance
	_ = cardkey.SaveKeyFile(keyDir, kf)

	// 2. 轮询云端状态直至 completed/failed/超时。
	events <- Event{Type: "stage", Stage: "cloud"}
	start := time.Now()
	deadline := start.Add(onlinePollTimeout)
	for {
		info, err := cli.GetTask(ctx, created.TaskID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 网络抖动不直接判失败:记日志,下一轮再试(上限仍受 6 小时约束)。
			events <- Event{Type: "log", Msg: fmt.Sprintf("查询云端状态失败(稍后重试): %v", err)}
		} else {
			switch info.Status {
			case "failed":
				if info.Error != "" {
					return fmt.Errorf("云端处理失败: %s", info.Error)
				}
				return errors.New("云端处理失败(服务端未给出原因)")
			case "completed":
				goto download
			default: // queued/processing
				events <- Event{Type: "log", Msg: fmt.Sprintf(
					"云端处理中(%s),已等待 %s", info.Status,
					time.Since(start).Round(time.Second))}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("云端处理超时(超过 %s 未完成),任务已失败;如已扣点请联系售卡方", onlinePollTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(onlinePollInterval):
		}
	}

download:
	// 3. 下载成片到本任务约定的输出位置(与本地管线一致: workDir/outName)。
	events <- Event{Type: "stage", Stage: "download"}
	events <- Event{Type: "log", Msg: "云端处理完成,下载成片..."}
	dst := filepath.Join(workDir, outName)
	if err := cli.Download(ctx, created.TaskID, dst, xferProgress("download", events)); err != nil {
		return fmt.Errorf("下载成片失败: %s", billing.Message(err))
	}
	if _, err := os.Stat(dst); err != nil {
		return fmt.Errorf("云端未产出 %s", outName)
	}
	events <- Event{Type: "log", Msg: "在线去字幕完成: " + dst}
	return nil
}
