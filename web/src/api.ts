export type Session = {
  username: string
  isAdmin: boolean
  apiUrl: string
  csrfToken: string
}

export type HubUser = {
  id: string
  username: string
  isAdmin: boolean
  disabled: boolean
}

export type RustDeskSetup = {
  idServer: string
  relayServer: string
  publicKey: string
  importConfig: string
}

export type RustDeskSettings = {
  idServer: string
  relayServer: string
  publicKey: string
  onlineEnabled: boolean
  onlineServer: string
}

export type Peer = {
  id: string
  alias: string
  note: string
  tags: string[]
  connection?: { url?: string; target: string }
}

export type Label = {
  id: string
  name: string
  color: number
}

export type Book = {
  total: number
  data: Peer[]
  tags: Label[]
}

export type DeviceStatus = 'online' | 'offline' | 'unknown'

export type OnlineStatus = {
  configured: boolean
  checkedAt: string | null
  statuses: Partial<Record<string, DeviceStatus>>
}

export type Diagnostic = {
  time: string
  source: 'rustdesk' | 'web'
  operation: string
  status: number
  errorCode?: string
  requestId: string
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
  }
}

type RequestOptions = {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  csrf?: string
  signal?: AbortSignal
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers({ Accept: 'application/json' })
  const body = options.body === undefined ? undefined : JSON.stringify(options.body)
  if (body !== undefined) {
    if (new TextEncoder().encode(body).byteLength > (1 << 20)) {
      throw new ApiError(413, 'Request.TooLarge', '提交内容超过 1 MiB，请缩短后重试')
    }
    headers.set('Content-Type', 'application/json')
  }
  if (options.csrf) headers.set('X-CSRF-Token', options.csrf)

  const timeout = AbortSignal.timeout(20_000)
  const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout
  let response: Response
  try {
    response = await fetch('/api/v1' + path, {
      method: options.method,
      headers,
      body,
      credentials: 'same-origin',
      signal,
    })
  } catch (error) {
    if (options.signal?.aborted) throw error
    throw new ApiError(0, 'Network.Unavailable', '连接失败，请稍后重试')
  }

  if (response.status === 204) return undefined as T
  const data = await response.json().catch(() => null)
  if (!response.ok || data === null) {
    throw new ApiError(
      response.status,
      data?.code ?? 'Server.ResponseFailed',
      data?.message ?? '无法读取服务响应，请稍后重试',
    )
  }
  return data as T
}

export function errorText(error: unknown, mutation = false): string {
  if (error instanceof ApiError) {
    if (mutation && (error.status === 0 || error.status >= 500 || error.status < 300)) {
      return '提交结果待确认。请刷新页面核对后再试。'
    }
    return error.message
  }
  return '操作失败，请稍后重试'
}
