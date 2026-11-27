import {
  ApiOutlined, ContactsOutlined, ControlOutlined, DesktopOutlined, LogoutOutlined, SettingOutlined, TeamOutlined,
} from '@ant-design/icons'
import { Alert, App, Avatar, Button, Form, Input, Spin, Tooltip } from 'antd'
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { PageErrorBoundary } from './PageErrorBoundary'
import { ApiError, errorText, request, type Session } from './api'

const AddressBook = lazy(() => import('./AddressBook').then((module) => ({ default: module.AddressBook })))
const ConnectionSettings = lazy(() => import('./Settings').then((module) => ({ default: module.ConnectionSettings })))
const AccountSettings = lazy(() => import('./Settings').then((module) => ({ default: module.AccountSettings })))
const UserManagement = lazy(() => import('./UserManagement').then((module) => ({ default: module.UserManagement })))
const SystemSettings = lazy(() => import('./SystemSettings').then((module) => ({ default: module.SystemSettings })))

const views = [
  { key: 'book', label: '个人通讯录', icon: <ContactsOutlined /> },
  { key: 'connection', label: '接入与诊断', icon: <ApiOutlined /> },
  { key: 'users', label: '用户管理', icon: <TeamOutlined /> },
  { key: 'system', label: '系统设置', icon: <ControlOutlined /> },
  { key: 'account', label: '账号设置', icon: <SettingOutlined /> },
] as const

type View = typeof views[number]['key']
type LoginFields = { username: string; password: string }

function readView(): View {
  return views.find((view) => '#' + view.key === window.location.hash)?.key ?? 'book'
}

function requiresAdmin(view: View): boolean {
  return view === 'users' || view === 'system'
}

function Brand() {
  return (
    <div className="brand">
      <span className="brand-icon" aria-hidden="true"><DesktopOutlined /></span>
      <span>RustDesk Hub</span>
    </div>
  )
}

function Login({ onLogin }: { onLogin: (session: Session) => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function login(values: LoginFields) {
    setBusy(true)
    setError('')
    try {
      onLogin(await request<Session>('/session', { method: 'POST', body: values }))
    } catch (failure) {
      setError(errorText(failure))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="login-screen">
      <section className="login-card" aria-labelledby="login-title">
        <Brand />
        <h1 id="login-title">登录管理端</h1>
        <p>使用与 RustDesk 客户端相同的账号登录。</p>
        {error && <Alert type="error" showIcon title={error} className="form-alert" />}
        <Form<LoginFields> layout="vertical" onFinish={login} requiredMark={false}>
          <Form.Item name="username" label="账号" rules={[{ required: true, message: '请输入账号' }]}>
            <Input autoFocus autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={busy}>登录</Button>
        </Form>
      </section>
    </main>
  )
}

export function HubApp() {
  const [view, setView] = useState<View>(readView)
  const [session, setSession] = useState<Session | null>(null)
  const [initializing, setInitializing] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [loggingOut, setLoggingOut] = useState(false)
  const sessionChannel = useRef<BroadcastChannel>(null)
  const { message } = App.useApp()

  const restoreSession = useCallback(() => {
    setSession(null)
    setLoadError('')
    setInitializing(true)
    setAttempt((value) => value + 1)
  }, [])

  useEffect(() => {
    function onPageShow(event: PageTransitionEvent) {
      if (event.persisted) restoreSession()
    }
    window.addEventListener('pageshow', onPageShow)
    const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('rustdesk-hub:session')
    if (channel) channel.onmessage = restoreSession
    sessionChannel.current = channel
    return () => {
      window.removeEventListener('pageshow', onPageShow)
      sessionChannel.current = null
      channel?.close()
    }
  }, [restoreSession])

  useEffect(() => {
    const change = () => setView(readView())
    window.addEventListener('hashchange', change)
    return () => window.removeEventListener('hashchange', change)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    request<Session>('/session', { signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) setSession(value)
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted && !(error instanceof ApiError && error.code === 'Session.Invalid')) {
          setLoadError(errorText(error))
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setInitializing(false)
      })
    return () => controller.abort()
  }, [attempt])

  const handleFailure = useCallback((error: unknown, mutation = false) => {
    if (error instanceof ApiError && error.code === 'Session.Changed') {
      restoreSession()
    } else if (error instanceof ApiError && error.code === 'Session.Invalid') {
      setSession(null)
      void message.warning('会话已失效，请重新登录')
    }
    return errorText(error, mutation)
  }, [message, restoreSession])

  function sessionChanged(next: Session | null) {
    setSession(next)
    sessionChannel.current?.postMessage(null)
  }

  async function logout() {
    if (!session) return
    setLoggingOut(true)
    try {
      await request('/session', { method: 'DELETE', csrf: session.csrfToken })
      sessionChanged(null)
    } catch (error) {
      void message.error(handleFailure(error, true))
    } finally {
      setLoggingOut(false)
    }
  }

  if (initializing) {
    return <main className="centered"><Spin aria-label="正在加载" size="large" /></main>
  }
  if (loadError) {
    return (
      <main className="centered">
        <Alert
          type="error"
          title={loadError}
          action={<Button onClick={restoreSession}>重试</Button>}
        />
      </main>
    )
  }
  if (!session) return <Login onLogin={sessionChanged} />

  return (
    <div className="hub-shell">
      <aside className="sidebar">
        <Brand />
        <nav className="navigation" aria-label="管理菜单">
          {views.filter((item) => session.isAdmin || !requiresAdmin(item.key)).map((item) => (
            <a
              key={item.key}
              href={'#' + item.key}
              aria-current={view === item.key ? 'page' : undefined}
            >
              <span className="nav-icon" aria-hidden="true">{item.icon}</span>
              <span>{item.key === 'connection' && !session.isAdmin ? '客户端接入' : item.label}</span>
            </a>
          ))}
        </nav>
        <div className="sidebar-account">
          <Avatar size={30} className="user-avatar" aria-hidden="true">
            {Array.from(session.username)[0]?.toUpperCase()}
          </Avatar>
          <span className="account-name" title={session.username}>{session.username}</span>
          <Tooltip title="退出登录">
            <Button
              className="logout-button"
              type="text"
              aria-label="退出登录"
              icon={<LogoutOutlined />}
              loading={loggingOut}
              onClick={logout}
            />
          </Tooltip>
        </div>
      </aside>
      <main className="content">
        <PageErrorBoundary key={view}>
          <Suspense fallback={<Spin aria-label="正在加载页面" />}>
            {view === 'book' && <AddressBook csrf={session.csrfToken} onFailure={handleFailure} />}
            {view === 'connection' && (
              <ConnectionSettings
                apiUrl={session.apiUrl}
                csrf={session.csrfToken}
                showDiagnostics={session.isAdmin}
                onFailure={handleFailure}
              />
            )}
            {view === 'users' && session.isAdmin && (
              <UserManagement csrf={session.csrfToken} onFailure={handleFailure} />
            )}
            {view === 'system' && session.isAdmin && (
              <SystemSettings apiUrl={session.apiUrl} csrf={session.csrfToken} onFailure={handleFailure} />
            )}
            {requiresAdmin(view) && !session.isAdmin && (
              <Alert type="warning" title="此页面需要管理员权限" action={<Button href="#book">返回通讯录</Button>} />
            )}
            {view === 'account' && (
              <AccountSettings
                session={session}
                onFailure={handleFailure}
                onChanged={() => {
                  sessionChanged(null)
                  void message.success('密码已更新，请重新登录')
                }}
              />
            )}
          </Suspense>
        </PageErrorBoundary>
      </main>
    </div>
  )
}
