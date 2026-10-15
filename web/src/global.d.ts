// highlightjs-line-numbers.js 是普通的浏览器脚本包：它自己往 window.hljs 上挂
// lineNumbersBlockSync 并读 window.hljs.versionString 做版本判断，没有自带类型。
// 这里只补本仓库实际用到的运行时形状，接口保持最小。
declare module 'highlightjs-line-numbers.js'

interface HljsLineNumbersOptions {
  singleLine?: boolean
}

interface Window {
  hljs?: {
    versionString?: string
    lineNumbersBlockSync?: (block: HTMLElement, options?: HljsLineNumbersOptions) => void
  }
}
