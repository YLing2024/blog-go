import test from 'node:test'
import assert from 'node:assert/strict'
import { formatDate, headingId } from '../src/utils.ts'

test('formatDate：空值一律返回空串', () => {
  assert.equal(formatDate(''), '')
  assert.equal(formatDate(null), '')
  assert.equal(formatDate(undefined), '')
  assert.equal(formatDate('   '), '')
})

test('formatDate：只取日期段，并把连字符换成点', () => {
  assert.equal(formatDate('2026-09-30 12:34:56'), '2026.09.30')
  assert.equal(formatDate('2026-01-02'), '2026.01.02')
})

test('formatDate：不补零，零填充由数据侧负责', () => {
  assert.equal(formatDate('2026-9-3 00:00:00'), '2026.9.3')
})

test('formatDate：忽略首尾空白', () => {
  assert.equal(formatDate('  2026-09-30  '), '2026.09.30')
  assert.equal(formatDate('\t2026-09-30\n'), '2026.09.30')
})

test('formatDate：非日期串按字面替换，不做解析', () => {
  assert.equal(formatDate('20260930'), '20260930')
  // 契约是 "YYYY-MM-DD HH:MM:SS"；带 T 的 ISO 串仅替换连字符（固化现存行为）
  assert.equal(formatDate('2026-09-30T12:34:56Z'), '2026.09.30T12:34:56Z')
})

test('headingId：生成纯序号锚点 sec-N', () => {
  assert.equal(headingId(0), 'sec-0')
  assert.equal(headingId(1), 'sec-1')
  assert.equal(headingId(42), 'sec-42')
})

test('headingId：相同输入稳定、不同输入不同、前缀固定', () => {
  assert.equal(headingId(7), headingId(7))
  assert.notEqual(headingId(1), headingId(2))
  assert.match(headingId(9), /^sec-\d+$/)
  // 负数边界（TOC 序号从 0 递增，不会出现；仅固化模板字符串行为）
  assert.equal(headingId(-1), 'sec--1')
})
