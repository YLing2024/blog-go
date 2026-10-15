// Package bcrypt 是以纯标准库实现的 bcrypt 口令哈希，字节级兼容旧实现所用的 bcryptjs。
//
// 之所以自带实现而不引第三方：本服务的依赖白名单只放开 SQLite / MySQL / Redis 驱动，
// 而口令哈希是安全敏感的基础件，移植一份可审计的小实现比新增依赖更可控。
// 算法与编码均对齐 bcryptjs（MIT）：修订号 $2a/$2b/$2y、成本 4–31、bcrypt 自有 base64 字母表。
package bcrypt

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"

	"blog-go/internal/blowfish"
)

// 成本取值域与 bcryptjs 一致。
const (
	MinCost     = 4
	MaxCost     = 31
	DefaultCost = 10
	saltLen     = 16
	hashLen     = 60
)

// bcryptAlphabet 是 bcrypt 的非标准 base64 字母表（'.' '/' 开头）。
const bcryptAlphabet = "./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// magic 是 bcrypt 编码前的固定明文（"OrpheanBeholderScryDoubt"）。
var magic = [6]uint32{0x4f727068, 0x65616e42, 0x65686f6c, 0x64657253, 0x63727944, 0x6f756274}

// base64Index 是 base64 字符到 6 位值的反查表（-1 表示非法字符）。
var base64Index = buildIndex()

func buildIndex() [128]int {
	var idx [128]int
	for i := range idx {
		idx[i] = -1
	}
	for i := 0; i < len(bcryptAlphabet); i++ {
		idx[bcryptAlphabet[i]] = i
	}
	return idx
}

// Hash 用给定成本（log2 轮数）哈希口令，返回 60 字符的 bcrypt 串。
func Hash(password string, cost int) (string, error) {
	if cost < MinCost || cost > MaxCost {
		return "", fmt.Errorf("bcrypt 成本必须在 %d–%d 之间", MinCost, MaxCost)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐值失败: %w", err)
	}
	saltStr := "$2a$" + twoDigits(cost) + "$" + base64Encode(salt, saltLen)
	return hashWithSalt(password, saltStr)
}

// Compare 校验口令与哈希是否匹配（哈希非法时返回 false，不抛错）。
func Compare(password, hash string) bool {
	if len(hash) != hashLen {
		return false
	}
	computed, err := hashWithSalt(password, hash[:29])
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1
}

// hashWithSalt 复刻 bcryptjs 的 _hash：解析盐串 → 昂贵编排 → 输出规范化哈希串。
func hashWithSalt(password, salt string) (string, error) {
	if len(salt) < 29 || salt[0] != '$' || salt[1] != '2' {
		return "", errors.New("bcrypt 盐串版本非法")
	}
	var minor byte
	var offset int
	if salt[2] == '$' {
		minor = 0
		offset = 3
	} else {
		minor = salt[2]
		if (minor != 'a' && minor != 'b' && minor != 'y') || salt[3] != '$' {
			return "", errors.New("bcrypt 盐串修订号非法")
		}
		offset = 4
	}
	cost, err := strconv.Atoi(salt[offset : offset+2])
	if err != nil {
		return "", errors.New("bcrypt 盐串缺少成本位")
	}
	realSalt := salt[offset+3 : offset+25]
	saltBytes, err := base64Decode(realSalt, saltLen)
	if err != nil {
		return "", err
	}

	phrase := []byte(password)
	if minor >= 'a' {
		// bcryptjs 对 2a/2b/2y 在口令末尾补一个 NUL；$2$ 不补。
		phrase = append(phrase, 0)
	}
	raw := crypt(phrase, saltBytes, cost)

	out := []byte{'$', '2'}
	if minor >= 'a' {
		out = append(out, minor)
	}
	out = append(out, '$')
	out = append(out, twoDigits(cost)...)
	out = append(out, '$')
	out = append(out, base64Encode(saltBytes, saltLen)...)
	out = append(out, base64Encode(raw, len(magic)*4-1)...)
	return string(out), nil
}

// crypt 是 bcrypt 核心：EksBlowfish 编排 2^cost 轮，再对固定明文加密 64 轮。
func crypt(password, salt []byte, cost int) []byte {
	c := blowfish.New()
	c.EksExpandKey(salt, password)
	rounds := 1 << uint(cost)
	for i := 0; i < rounds; i++ {
		c.ExpandKey(password)
		c.ExpandKey(salt)
	}
	cdata := magic
	for i := 0; i < 64; i++ {
		cdata[0], cdata[1] = c.Encipher(cdata[0], cdata[1])
		cdata[2], cdata[3] = c.Encipher(cdata[2], cdata[3])
		cdata[4], cdata[5] = c.Encipher(cdata[4], cdata[5])
	}
	out := make([]byte, 0, 24)
	for _, w := range cdata {
		out = append(out, byte(w>>24), byte(w>>16), byte(w>>8), byte(w))
	}
	return out
}

// twoDigits 把成本格式化为两位十进制（bcrypt 串里成本恒为两位）。
func twoDigits(n int) string {
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}

// base64Encode 是 bcrypt 自有 base64 编码，最多编码 maxLen 字节，无填充。
func base64Encode(data []byte, maxLen int) string {
	if maxLen <= 0 || maxLen > len(data) {
		return ""
	}
	out := make([]byte, 0, (maxLen*4+2)/3)
	off := 0
	for off < maxLen {
		c1 := int(data[off])
		off++
		out = append(out, bcryptAlphabet[(c1>>2)&0x3f])
		c1 = (c1 & 0x03) << 4
		if off >= maxLen {
			out = append(out, bcryptAlphabet[c1&0x3f])
			break
		}
		c2 := int(data[off])
		off++
		c1 |= (c2 >> 4) & 0x0f
		out = append(out, bcryptAlphabet[c1&0x3f])
		c1 = (c2 & 0x0f) << 2
		if off >= maxLen {
			out = append(out, bcryptAlphabet[c1&0x3f])
			break
		}
		c2 = int(data[off])
		off++
		c1 |= (c2 >> 6) & 0x03
		out = append(out, bcryptAlphabet[c1&0x3f], bcryptAlphabet[c2&0x3f])
	}
	return string(out)
}

// base64Decode 是 bcrypt 自有 base64 解码，最多解出 maxLen 字节。
func base64Decode(s string, maxLen int) ([]byte, error) {
	if maxLen <= 0 {
		return nil, errors.New("bcrypt base64 目标长度非法")
	}
	out := make([]byte, 0, maxLen)
	off, olen := 0, 0
	for off < len(s)-1 && olen < maxLen {
		c1 := indexOf(s[off])
		off++
		c2 := indexOf(s[off])
		off++
		if c1 < 0 || c2 < 0 {
			break
		}
		out = append(out, byte((c1<<2)|((c2&0x30)>>4)))
		olen++
		if olen >= maxLen || off >= len(s) {
			break
		}
		c3 := indexOf(s[off])
		off++
		if c3 < 0 {
			break
		}
		out = append(out, byte(((c2&0x0f)<<4)|((c3&0x3c)>>2)))
		olen++
		if olen >= maxLen || off >= len(s) {
			break
		}
		c4 := indexOf(s[off])
		off++
		out = append(out, byte(((c3&0x03)<<6)|c4))
		olen++
	}
	return out, nil
}

// indexOf 返回 bcrypt base64 字符对应的 6 位值；非法返回 -1。
func indexOf(ch byte) int {
	if int(ch) < len(base64Index) {
		return base64Index[ch]
	}
	return -1
}
