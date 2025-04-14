package rpc

import (
	"errors"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/sasha-s/go-deadlock"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
	"time"
)

const (
	keyIntv = int64(60000) // Repeat message sending interval, unit: seconds 重复消息发送间隔，单位：秒
)

var (
	keyLastMap = map[string]int64{}
	lockExcKey deadlock.Mutex
)

func NewExcNotify() *ExcNotify {
	level := zap.NewAtomicLevel()
	_ = level.UnmarshalText([]byte("ERROR"))
	encoder := log.NewTextEncoder(log.NewEncoderConfig(), false, false)
	return &ExcNotify{
		LevelEnabler: level,
		enc: &ExcEncoder{
			TextEncoder: encoder.(*log.TextEncoder),
		},
	}
}

type ExcNotify struct {
	zapcore.LevelEnabler
	enc *ExcEncoder
}

type ExcEncoder struct {
	*log.TextEncoder
}

func (h *ExcNotify) With(fields []zapcore.Field) zapcore.Core {
	clone := h.clone()
	clone.enc.addFields(fields)
	return clone
}

func (h *ExcNotify) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if h.Enabled(ent.Level) {
		return ce.AddCore(ent, h)
	}
	return ce
}

func (h *ExcNotify) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	callerStr := ent.Caller.TrimmedPath()
	buf, _ := h.enc.EncodeEntry(ent, fields)
	TrySendExc(callerStr, buf.String())
	return nil
}

func (h *ExcNotify) Sync() error {
	return nil
}

func (h *ExcNotify) clone() *ExcNotify {
	return &ExcNotify{
		LevelEnabler: h.LevelEnabler,
		enc:          h.enc.Clone(),
	}
}

func (e *ExcEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	final := e.Clone()
	final.AppendString(ent.Time.Format("2006/01/02 15:04"))
	final.Buf.AppendByte(' ')
	final.AppendString(ent.Message)
	if e.Buf.Len() > 0 {
		final.Buf.AppendByte(' ')
		final.Buf.Write(e.Buf.Bytes())
	}
	final.addFields(fields)
	ret := final.Buf
	final.Reset()
	return ret, nil
}

func (e *ExcEncoder) Clone() *ExcEncoder {
	encoder := e.TextEncoder.Clone()
	return &ExcEncoder{
		TextEncoder: encoder.(*log.TextEncoder),
	}
}

func (e *ExcEncoder) addFields(fields []zapcore.Field) {
	for _, f := range fields {
		if f.Type == zapcore.ErrorType {
			e.Buf.AppendByte(' ')
			err := f.Interface.(error)
			var err2 *errs.Error
			if errors.As(err, &err2) {
				e.AddString(f.Key, err2.Short())
			} else {
				e.AddString(f.Key, err.Error())
			}
			continue
		}
		f.AddTo(e)
	}
}

func TrySendExc(cacheKey string, content string) {
	if core.Cache == nil {
		err := core.Setup()
		if err != nil {
			log.Error("run core.Setup fail", zap.Error(err))
			return
		}
	}
	ttl, valid := core.Cache.GetTTL(cacheKey)
	var numVal int
	if valid {
		// With content, number of updates and last message. 有内容，更新数量和最后的消息
		numVal = core.GetCacheVal(cacheKey, 0)
	} else {
		// No content, save and set the sending time. 没有内容，保存并设置发送时间
		ttl = time.Millisecond * time.Duration(keyIntv)
	}
	core.Cache.SetWithTTL(cacheKey, numVal+1, 1, ttl)
	core.Cache.SetWithTTL(cacheKey+"_text", content, 1, ttl)
	lockExcKey.Lock()
	lastMS, ok := keyLastMap[cacheKey]
	lockExcKey.Unlock()
	curMS := btime.UTCStamp()
	waitMS := int64(0)
	if !ok || curMS-lastMS > keyIntv {
		// The distance from the last time has exceeded the interval, send immediately
		// 距离上次已超过间隔，立刻发送
		sendExcAfter(1000, cacheKey)
		// Update the next execution time
		// 更新下次执行时间
		waitMS = keyIntv
	} else if curMS < lastMS {
		// Existing deployed timer, exit
		// 已有部署的定时器，退出
		return
	} else {
		// There is no timer, it has not reached the next time yet
		// 没有定时器，还未达到下次时间
		waitMS = lastMS + keyIntv - 1000 - curMS
	}
	lockExcKey.Lock()
	keyLastMap[cacheKey] = curMS + waitMS
	lockExcKey.Unlock()
	sendExcAfter(waitMS, cacheKey)
}

func sendExcAfter(waitMS int64, key string) {
	waits := time.Millisecond * time.Duration(waitMS)
	time.AfterFunc(waits, func() {
		textKey := key + "_text"
		num := core.GetCacheVal(key, 0)
		content := core.GetCacheVal(textKey, "")
		core.Cache.Del(key)
		core.Cache.Del(textKey)
		if num == 0 {
			return
		}
		text := fmt.Sprintf("num:%d, %s\n%s", num, key, content)
		SendMsg(map[string]interface{}{
			"type":   MsgTypeException,
			"status": text,
		})
	})
}
