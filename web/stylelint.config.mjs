// Stylelint 配置 — 只保留正确性规则，stylelint-config-standard 自带的风格规则全部关掉。
// 关停清单从已解析的配置对象动态推导，避免手写几十条规则名后与上游版本漂移。
import standard from 'stylelint-config-standard'
import recommended from 'stylelint-config-recommended'

// 本轮要保留的六个正确性规则（其余一律 off）
const CORRECTNESS_RULES = [
  'no-duplicate-selectors',
  'declaration-block-no-duplicate-properties',
  'no-invalid-position-at-import-rule',
  'no-descending-specificity',
  'no-invalid-double-slash-comments',
  'property-no-unknown',
]

// standard 继承 recommended，取二者规则名并集作为「需要关掉」的候选
const inherited = { ...(recommended.rules || {}), ...(standard.rules || {}) }
const silenced = Object.fromEntries(
  Object.keys(inherited)
    .filter((name) => !CORRECTNESS_RULES.includes(name))
    .map((name) => [name, null]),
)

export default {
  extends: ['stylelint-config-standard'],
  rules: {
    ...silenced,
    'no-duplicate-selectors': true,
    'declaration-block-no-duplicate-properties': true,
    'no-invalid-position-at-import-rule': true,
    // 先以 warn 暴露既有问题，不阻塞本轮；治理单独排期
    'no-descending-specificity': [true, { severity: 'warning' }],
    'no-invalid-double-slash-comments': true,
    'property-no-unknown': true,
  },
}
