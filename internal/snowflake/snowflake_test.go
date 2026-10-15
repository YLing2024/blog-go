package snowflake

import (
	"strconv"
	"testing"
)

// decodeParts 从 ID 里解出时间戳（毫秒）、机器号与序列，用于断言。
func decodeParts(t *testing.T, id string) (int64, int64, int64) {
	t.Helper()
	v, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		t.Fatalf("ID 不是十进制: %v", err)
	}
	ms := int64(v >> (workerBits + seqBits))
	worker := int64((v >> seqBits) & uint64(maxWorker))
	seq := int64(v & uint64(maxSeq))
	return ms + EPOCH, worker, seq
}

func TestNextIsMonotonicAndTimeOrdered(t *testing.T) {
	g := New(7)
	prev := g.Next()
	for i := 0; i < 1000; i++ {
		cur := g.Next()
		if cur <= prev {
			t.Fatalf("ID 未单调递增: %s -> %s", prev, cur)
		}
		prev = cur
	}
}

func TestNextAtReflectsGivenTime(t *testing.T) {
	g := New(0)
	id := g.NextAt(1600000000000)
	ms, worker, _ := decodeParts(t, id)
	if ms != 1600000000000 {
		t.Fatalf("时间戳不符: got %d", ms)
	}
	if worker != 0 {
		t.Fatalf("机器号不符: %d", worker)
	}
}

func TestClockRollbackKeepsMonotonic(t *testing.T) {
	g := New(3)
	first := g.NextAt(1700000000000)
	second := g.NextAt(1600000000000) // 回拨
	if second <= first {
		t.Fatalf("时钟回拨后应仍递增: %s -> %s", first, second)
	}
}

func TestWorkerClamp(t *testing.T) {
	g := New(99999)
	id := g.Next()
	_, worker, _ := decodeParts(t, id)
	if worker != maxWorker {
		t.Fatalf("机器号应被夹到 %d，得到 %d", maxWorker, worker)
	}
}
