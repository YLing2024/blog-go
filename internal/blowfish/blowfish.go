// Package blowfish 是 Blowfish 分组密码的最小实现，仅服务于 bcrypt。
//
// 与通用 Blowfish 库不同，这里刻意保留 bcrypt 的「昂贵密钥编排」（EksBlowfish）：
// P-array 与 S-box 用 π 的初值起步，先与口令异或，再借助盐值反复重编排。
// 算法逐行移植自 bcryptjs（MIT）的 src/bcrypt/impl.js，保证与旧实现产出的哈希字节级一致。
package blowfish

// Cipher 保存可变的 P-array 与 4 组 S-box（bcrypt 每轮都会改写它们）。
type Cipher struct {
	p [18]uint32
	s [1024]uint32
}

// New 返回以 π 初值重置的密码实例。
func New() *Cipher {
	c := &Cipher{}
	c.p = pOrig
	c.s = sOrig
	return c
}

// Encipher 加密一对 32 位字，返回 (lr0, lr1)。
func (c *Cipher) Encipher(l, r uint32) (uint32, uint32) {
	l ^= c.p[0]
	for i := 0; i < 16; i += 2 {
		r ^= c.feistel(l) ^ c.p[i+1]
		l ^= c.feistel(r) ^ c.p[i+2]
	}
	return r ^ c.p[17], l
}

// ExpandKey 是普通 Blowfish 密钥编排（bcrypt 轮函数里对 0 盐的 _key）。
func (c *Cipher) ExpandKey(key []byte) {
	offset := 0
	var word uint32
	for i := 0; i < 18; i++ {
		word, offset = streamToWord(key, offset)
		c.p[i] ^= word
	}
	var l, r uint32
	for i := 0; i < 18; i += 2 {
		l, r = c.Encipher(l, r)
		c.p[i], c.p[i+1] = l, r
	}
	for i := 0; i < 1024; i += 2 {
		l, r = c.Encipher(l, r)
		c.s[i], c.s[i+1] = l, r
	}
}

// EksExpandKey 是 bcrypt 的昂贵密钥编排（_ekskey）：先用口令异或 P-array，
// 再逐字吸收盐值并重编排，使密钥编排随盐值变化。
func (c *Cipher) EksExpandKey(salt, key []byte) {
	offset := 0
	var word uint32
	for i := 0; i < 18; i++ {
		word, offset = streamToWord(key, offset)
		c.p[i] ^= word
	}
	offset = 0
	var l, r uint32
	for i := 0; i < 18; i += 2 {
		var w uint32
		w, offset = streamToWord(salt, offset)
		l ^= w
		w, offset = streamToWord(salt, offset)
		r ^= w
		l, r = c.Encipher(l, r)
		c.p[i], c.p[i+1] = l, r
	}
	for i := 0; i < 1024; i += 2 {
		var w uint32
		w, offset = streamToWord(salt, offset)
		l ^= w
		w, offset = streamToWord(salt, offset)
		r ^= w
		l, r = c.Encipher(l, r)
		c.s[i], c.s[i+1] = l, r
	}
}

// feistel 是 Blowfish 的 F 函数。
func (c *Cipher) feistel(x uint32) uint32 {
	n := c.s[x>>24]
	n += c.s[0x100|((x>>16)&0xff)]
	n ^= c.s[0x200|((x>>8)&0xff)]
	n += c.s[0x300|(x&0xff)]
	return n
}

// streamToWord 从 data 循环取 4 字节拼成一个 32 位字（大端），返回新的读取偏移。
func streamToWord(data []byte, offset int) (uint32, int) {
	var word uint32
	for i := 0; i < 4; i++ {
		word = (word << 8) | uint32(data[offset])
		offset = (offset + 1) % len(data)
	}
	return word, offset
}
