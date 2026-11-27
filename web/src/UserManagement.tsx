import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, App, Badge, Button, Form, Input, Modal, Popconfirm, Space, Table } from 'antd'
import { useEffect, useState } from 'react'
import { request, type HubUser } from './api'
import { confirmPasswordRules, newPasswordRules } from './passwordRules'

type Props = {
  csrf: string
  onFailure: (error: unknown, mutation?: boolean) => string
}

type EditUser = { kind: 'create' } | { kind: 'reset'; user: HubUser }

export function UserManagement({ csrf, onFailure }: Props) {
  const [users, setUsers] = useState<HubUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const [editor, setEditor] = useState<EditUser | null>(null)
  const [changingStatus, setChangingStatus] = useState(false)
  const { message } = App.useApp()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    request<HubUser[]>('/users', { csrf, signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) setUsers(value)
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) setError(onFailure(failure))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [csrf, onFailure, revision])

  function reload() {
    setRevision((value) => value + 1)
  }

  async function changeStatus(user: HubUser) {
    setChangingStatus(true)
    try {
      await request('/users/' + user.id, { method: 'PATCH', csrf, body: { disabled: !user.disabled } })
      void message.success(user.disabled ? '用户已启用' : '用户已停用')
      reload()
    } catch (failure) {
      void message.error(onFailure(failure, true))
    } finally {
      setChangingStatus(false)
    }
  }

  return (
    <>
      <div className="page-heading">
        <h1>用户管理</h1>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditor({ kind: 'create' })}>新建用户</Button>
      </div>
      <section className="panel" aria-label="用户列表">
        <div className="book-tools">
          <h2>用户列表</h2>
          <Button icon={<ReloadOutlined />} onClick={reload}>刷新</Button>
        </div>
        {error && <Alert className="table-alert" type="error" title={error} showIcon />}
        <Table<HubUser>
          rowKey="id"
          dataSource={users}
          loading={loading}
          scroll={{ x: 550 }}
          pagination={{ pageSize: 10, hideOnSinglePage: true, showSizeChanger: false }}
          columns={[
            { title: '账号', render: (_, user) => <strong className="managed-username">{user.username}</strong> },
            { title: '角色', width: 100, render: (_, user) => user.isAdmin ? '管理员' : '普通用户' },
            {
              title: '状态', width: 100, render: (_, user) => (
                <Badge status={user.disabled ? 'default' : 'success'} text={user.disabled ? '已停用' : '已启用'} />
              ),
            },
            {
              title: '操作', width: 180, render: (_, user) => user.isAdmin ? '—' : (
                <Space size={4}>
                  <Button type="text" size="small" disabled={changingStatus} onClick={() => setEditor({ kind: 'reset', user })}>
                    重置密码
                  </Button>
                  {user.disabled ? (
                    <Button type="text" size="small" disabled={changingStatus} onClick={() => void changeStatus(user)}>启用</Button>
                  ) : (
                    <Popconfirm
                      title={`停用「${user.username}」？`}
                      description="该用户的网页和 RustDesk 客户端会话将失效。"
                      okText="停用"
                      onConfirm={() => changeStatus(user)}
                      disabled={changingStatus}
                    >
                      <Button type="text" size="small" danger disabled={changingStatus}>停用</Button>
                    </Popconfirm>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </section>
      {editor && (
        <UserEditor
          editor={editor}
          csrf={csrf}
          onFailure={onFailure}
          onClose={() => setEditor(null)}
          onSaved={(text) => {
            setEditor(null)
            void message.success(text)
            reload()
          }}
        />
      )}
    </>
  )
}

type UserFields = { username: string; password: string; confirmPassword: string }

function UserEditor({ editor, csrf, onFailure, onClose, onSaved }: Props & {
  editor: EditUser
  onClose: () => void
  onSaved: (text: string) => void
}) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function save({ username, password }: UserFields) {
    setSaving(true)
    setError('')
    try {
      if (editor.kind === 'create') {
        await request('/users', { method: 'POST', csrf, body: { username, password } })
        onSaved('用户已新建')
      } else {
        await request('/users/' + editor.user.id + '/password', { method: 'PUT', csrf, body: { password } })
        onSaved('密码已重置')
      }
    } catch (failure) {
      setError(onFailure(failure, true))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      title={editor.kind === 'create' ? '新建用户' : `重置「${editor.user.username}」的密码`}
      footer={null}
      onCancel={onClose}
      closable={!saving}
      mask={{ closable: !saving }}
      keyboard={!saving}
    >
      {editor.kind === 'reset' && <p className="form-alert">保存后，该用户需在网页和 RustDesk 客户端重新登录。</p>}
      {error && <Alert type="error" title={error} showIcon className="form-alert" />}
      <Form<UserFields> layout="vertical" onFinish={save} disabled={saving}>
        {editor.kind === 'create' && (
          <Form.Item name="username" label="账号" rules={[{ required: true, whitespace: true, message: '请输入账号' }]}>
            <Input autoComplete="off" />
          </Form.Item>
        )}
        <Form.Item name="password" label={editor.kind === 'create' ? '初始密码' : '新密码'} rules={newPasswordRules}>
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <Form.Item
          name="confirmPassword"
          label="确认密码"
          dependencies={['password']}
          rules={confirmPasswordRules('password')}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <div className="dialog-actions">
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" htmlType="submit" loading={saving}>{editor.kind === 'create' ? '新建' : '重置密码'}</Button>
        </div>
      </Form>
    </Modal>
  )
}
