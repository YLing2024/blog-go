import { useCallback, useEffect, useState } from 'react'

// 异步加载的三个状态；用联合字面量表达，避免 enum
export type AsyncStatus = 'loading' | 'success' | 'error'

interface AsyncState<T> {
  status: AsyncStatus
  data: T | null
}

export interface AsyncResult<T> extends AsyncState<T> {
  retry: () => void
}

export default function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncResult<T> {
  const [state, setState] = useState<AsyncState<T>>({ status: 'loading', data: null })
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let alive = true
    setState({ status: 'loading', data: null })
    fn().then(
      (data) => alive && setState({ status: 'success', data }),
      () => alive && setState({ status: 'error', data: null }),
    )
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tick, ...deps])

  const retry = useCallback(() => setTick((t) => t + 1), [])
  return { ...state, retry }
}
