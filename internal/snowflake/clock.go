package snowflake

import "time"

// nowMillis 是默认时钟；抽成变量便于测试注入。
var nowMillis = func() int64 { return time.Now().UnixMilli() }
