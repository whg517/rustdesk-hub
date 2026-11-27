import { CopyOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, Button, Form, Input, Spin, Table, Tag, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { request, type Diagnostic, type RustDeskSetup, type Session } from './api'
import { confirmPasswordRules, newPasswordRules } from './passwordRules'
import { useCopyText } from './useCopyText'

type FailureHandler = (error: unknown, mutation?: boolean) => string

const dateTime = new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' })
const operations: Record<string, string> = {
  login: '登录',
  logout: '退出',
  'session.read': '恢复会话',
  'password.change': '修改密码',
  'addressBook.read': '读取通讯录',
  'addressBook.write': '写入通讯录',
  'user.create': '新建用户',
  'user.status': '修改用户状态',
  'user.password': '重置用户密码',
  'settings.rustdesk': '保存 RustDesk 配置',
}

function resultText(row: Diagnostic): string {
  if (row.status < 300) return '成功'
  if (row.errorCode === 'Account.InvalidCredentials') return '凭据错误'
  if (row.errorCode === 'Session.Invalid') return '会话失效'
  if (row.status === 401) return row.operation === 'login' ? '凭据错误' : '认证失败'
  if (row.status === 413) return '请求体过大'
  if (row.status === 429) return '请求限流'
  if (row.status >= 500) return '服务错误'
  return '请求失败'
}

export function ConnectionSettings({ apiUrl, csrf, showDiagnostics, onFailure }: {
  apiUrl: string
  csrf: string
  showDiagnostics: boolean
  onFailure: FailureHandler
}) {
  const copyText = useCopyText()
  const [rustdesk, setRustdesk] = useState<RustDeskSetup>()
  const [loadingSetup, setLoadingSetup] = useState(true)
  const [setupError, setSetupError] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    request<{ rustdesk?: RustDeskSetup }>('/client-setup', { csrf, signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) setRustdesk(value.rustdesk)
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) setSetupError(onFailure(failure))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoadingSetup(false)
      })
    return () => controller.abort()
  }, [csrf, onFailure])

  const apiAddress = (
    <>
      <label htmlFor="api-address">API 服务器地址</label>
      <div className="copy-address">
        <Input id="api-address" readOnly value={apiUrl} />
        <Button icon={<CopyOutlined />} onClick={() => void copyText(apiUrl, 'API 地址')}>复制地址</Button>
      </div>
    </>
  )

  return (
    <>
      <div className="page-heading"><h1>{showDiagnostics ? '接入与诊断' : '客户端接入'}</h1></div>
      <section className="panel settings-panel" aria-labelledby="connection-title">
        <h2 id="connection-title">服务器配置</h2>
        {loadingSetup && <Spin size="small" aria-label="正在读取 RustDesk 配置" />}
        {setupError && <Alert type="error" title={setupError} showIcon className="form-alert" />}
        {rustdesk ? (
          <Button
            type="primary"
            icon={<CopyOutlined />}
            onClick={() => void copyText(rustdesk.importConfig, 'RustDesk 配置')}
          >
            复制 RustDesk 配置
          </Button>
        ) : (
          <>
            {apiAddress}
            {!loadingSetup && !setupError && (
              <p className="sync-fields">
                {showDiagnostics ? (
                  <>在<a href="#system">「系统设置」</a>填写 ID 服务器和公钥后，可复制完整配置。</>
                ) : '管理员配置 ID 服务器和公钥后，可复制完整配置。'}
              </p>
            )}
          </>
        )}
        {apiUrl.startsWith('http:') && (
          <Alert className="form-alert" type="info" showIcon title="当前地址用于本机预览。跨设备接入时使用部署后的 HTTPS 地址。" />
        )}
        <ol className="connection-steps">
          <li>
            打开 RustDesk 1.5.3 的「设置 → 网络 → ID/中继服务器」（ID/Relay server），
            {rustdesk ? '点击弹窗标题旁的「导入服务器配置」（Import server config），确认保存。' : '在弹窗中填写「API 服务器」。'}
          </li>
          <li>在「账号」页用本管理端账号登录，打开「个人通讯录」。</li>
          <li>远程连接使用自建 OSS 的 ID 服务器、中继服务器、公钥和设备授权。</li>
        </ol>
        {rustdesk && (
          <details className="manual-settings">
            <summary>手动配置</summary>
            <dl className="rustdesk-fields">
              <dt>ID 服务器</dt><dd>{rustdesk.idServer}</dd>
              <dt>中继服务器</dt><dd>{rustdesk.relayServer || '自动选择（留空）'}</dd>
              <dt>Key</dt><dd>{rustdesk.publicKey}</dd>
            </dl>
            {apiAddress}
          </details>
        )}
        <p className="sync-fields">通讯录同步设备 ID、别名、标签和备注。远控密码由 RustDesk 客户端管理。</p>
      </section>
      {showDiagnostics && <RecentRequests csrf={csrf} onFailure={onFailure} />}
    </>
  )
}

function RecentRequests({ csrf, onFailure }: { csrf: string; onFailure: FailureHandler }) {
  const [rows, setRows] = useState<Diagnostic[]>([])
  const [requestID, setRequestID] = useState('')
  const [revision, setRevision] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    request<Diagnostic[]>('/diagnostics', { csrf, signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) setRows(value)
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) setError(onFailure(failure))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [csrf, onFailure, revision])

  return (
    <section className="panel" aria-labelledby="diagnostic-title">
      <div className="book-tools">
        <h2 id="diagnostic-title">最近请求</h2>
        <div className="book-filters">
          <Input.Search
            className="book-search"
            aria-label="按请求 ID 查找"
            placeholder="按请求 ID 查找"
            allowClear
            onSearch={(value) => setRequestID(value.trim().toUpperCase())}
          />
          <Button icon={<ReloadOutlined />} onClick={() => setRevision((value) => value + 1)}>刷新</Button>
        </div>
      </div>
      {error && <Alert className="table-alert" type="error" showIcon title={error} />}
      <Table<Diagnostic>
        rowKey="requestId"
        dataSource={requestID ? rows.filter((row) => row.requestId.startsWith(requestID)) : rows}
        loading={loading}
        scroll={{ x: 580 }}
        locale={{ emptyText: requestID ? '未找到匹配的请求' : '暂无请求记录' }}
        pagination={{ pageSize: 8, showSizeChanger: false, hideOnSinglePage: true }}
        columns={[
          { title: '时间', dataIndex: 'time', render: (value: string) => dateTime.format(new Date(value)) },
          { title: '来源', dataIndex: 'source', render: (value: Diagnostic['source']) => value === 'web' ? '网页' : 'RustDesk' },
          { title: '操作', dataIndex: 'operation', render: (value: string) => operations[value] ?? value },
          { title: '结果', render: (_, row) => (
            <Tag color={row.status < 300 ? 'success' : 'error'}>{row.status} {resultText(row)}</Tag>
          ) },
          { title: '请求 ID', dataIndex: 'requestId', render: (value: string) => (
            <Typography.Text title={value} copyable={{ text: value }}>{value.slice(0, 8)}</Typography.Text>
          ) },
        ]}
      />
    </section>
  )
}

type PasswordFields = { currentPassword: string; newPassword: string; confirmPassword: string }

export function AccountSettings({
  session, onFailure, onChanged,
}: { session: Session; onFailure: FailureHandler; onChanged: () => void }) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function save(values: PasswordFields) {
    setSaving(true)
    setError('')
    try {
      await request('/account/password', {
        method: 'PUT',
        csrf: session.csrfToken,
        body: { currentPassword: values.currentPassword, newPassword: values.newPassword },
      })
      onChanged()
    } catch (failure) {
      setError(onFailure(failure, true))
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="page-heading"><h1>账号设置</h1></div>
      <section className="panel settings-panel password-panel" aria-labelledby="password-title">
        <p className="account-summary">当前账号 <strong>{session.username}</strong></p>
        <h2 id="password-title">修改密码</h2>
        <p>修改后，所有已登录的网页和客户端都需要重新登录。</p>
        {error && <Alert className="form-alert" type="error" showIcon title={error} />}
        <Form<PasswordFields> layout="vertical" onFinish={save} disabled={saving}>
          <Form.Item name="currentPassword" label="当前密码" rules={[{ required: true, message: '请输入当前密码' }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            name="newPassword"
            label="新密码"
            rules={newPasswordRules}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="confirmPassword"
            label="确认新密码"
            dependencies={['newPassword']}
            rules={confirmPasswordRules('newPassword')}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>修改密码</Button>
        </Form>
      </section>
    </>
  )
}
