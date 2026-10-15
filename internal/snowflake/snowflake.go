// Package snowflake 生成 64 位有序 ID：41 位毫秒时间戳 + 10 位机器号 + 12 位序列。
//
// 用途是文章 / 合集的对外标识（URL 里的 19 位数字）。与自增 id 的区别：时间有序、
// 单机内不连续不可枚举。始终以字符串返回——19 位数字超出 JavaScript Number 安全整数范围。
// 逐行对齐旧实现 server/src/snowflake.ts，包括时钟回拨时沿用上次时间戳的取舍。
package snowflake

import (
	"strconv"
	"sync"
)

// EPOCH 取 Twitter snowflake 标准纪元（2010-11-04），41 位毫秒可用到 2079 年。
const EPOCH int64 = 1288834974657

const (
	workerBits = 10
	seqBits    = 12
	maxSeq     = int64(1)<<seqBits - 1
	maxWorker  = int64(1)<<workerBits - 1
)

// Generator 是线程安全的雪花 ID 发号器。
type Generator struct {
	mu     sync.Mutex
	worker int64
	lastMs int64
	seq    int64
}

// New 构造发号器，workerID 会被夹到 [0,1023]（对齐旧实现的钳制语义）。
func New(workerID int) *Generator {
	w := int64(workerID)
	if w < 0 {
		w = 0
	}
	if w > maxWorker {
		w = maxWorker
	}
	return &Generator{worker: w, lastMs: -1}
}

// Next 生成下一个 ID（全局单调递增）。
func (g *Generator) Next() string {
	return g.NextAt(nowMillis())
}

// NextAt 按指定毫秒生成 ID（迁移回填用：让老数据的 ID 反映其创建时间）。
func (g *Generator) NextAt(ms int64) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ms < EPOCH {
		ms = EPOCH
	}
	if ms < g.lastMs {
		ms = g.lastMs // 时钟回拨：沿用上次时间戳，宁可慢一拍也不产生重复
	}
	if ms == g.lastMs {
		g.seq = (g.seq + 1) & maxSeq
		if g.seq == 0 {
			ms = g.lastMs + 1
		}
	} else {
		g.seq = 0
	}
	g.lastMs = ms
	return g.compose(ms, g.seq)
}

// compose 把时间戳、机器号、序列拼成十进制字符串。
func (g *Generator) compose(ms, seq int64) string {
	v := (uint64(ms-EPOCH) << (workerBits + seqBits)) | (uint64(g.worker) << seqBits) | uint64(seq)
	return strconv.FormatUint(v, 10)
}
