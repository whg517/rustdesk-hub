import { Alert, App, Button, Form, Input, Switch } from 'antd'
import { useEffect, useState } from 'react'
import { request, type RustDeskSettings } from './api'

const settingsPath = '/system-settings/rustdesk'
const emptySettings: RustDeskSettings = {
  idServer: '',
  relayServer: '',
  publicKey: '',
  onlineEnabled: false,
  onlineServer: '',
}

type Props = {
  apiUrl: string
  csrf: string
  onFailure: (error: unknown, mutation?: boolean) => string
}

export function SystemSettings({ apiUrl, csrf, onFailure }: Props) {
  const [form] = Form.useForm<RustDeskSettings>()
  const onlineEnabled = Form.useWatch('onlineEnabled', form)
  const [ready, setReady] = useState(false)
  const [revision, setRevision] = useState(0)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const { message } = App.useApp()

  useEffect(() => {
    const controller = new AbortController()
    setError('')
    request<RustDeskSettings>(settingsPath, { csrf, signal: controller.signal })
      .then((values) => {
        if (controller.signal.aborted) return
        form.setFieldsValue(values)
        setReady(true)
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) setError(onFailure(failure))
      })
    return () => controller.abort()
  }, [csrf, form, onFailure, revision])

  async function save(values: RustDeskSettings) {
    setSaving(true)
    setError('')
    try {
      const saved = await request<RustDeskSettings>(settingsPath, { method: 'PUT', body: values, csrf })
      form.setFieldsValue(saved)
      void message.success('RustDesk 配置已保存')
    } catch (failure) {
      setError(onFailure(failure, true))
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="page-heading"><h1>系统设置</h1></div>
      <section className="panel settings-panel system-settings-panel" aria-labelledby="rustdesk-settings-title">
        <h2 id="rustdesk-settings-title">RustDesk 服务器</h2>
        {error && (
          <Alert
            type="error"
            title={error}
            showIcon
            className="form-alert"
            action={!ready && <Button onClick={() => setRevision((value) => value + 1)}>重试</Button>}
          />
        )}
        <Form<RustDeskSettings>
          form={form}
          layout="vertical"
          initialValues={emptySettings}
          disabled={!ready || saving}
          onFinish={save}
        >
          <Form.Item name="idServer" label="ID 服务器">
            <Input placeholder="rustdesk.example.com:21116" autoComplete="off" />
          </Form.Item>
          <Form.Item name="relayServer" label="中继服务器" extra="留空时由 RustDesk 自动选择中继。">
            <Input placeholder="rustdesk.example.com:21117" autoComplete="off" />
          </Form.Item>
          <Form.Item name="publicKey" label="服务端公钥" extra="填写 RustDesk 服务器的 id_ed25519.pub 内容。">
            <Input autoComplete="off" spellCheck={false} />
          </Form.Item>
          <Form.Item label="API 服务器地址" htmlFor="system-api-url" extra="由 Hub 的访问地址生成。">
            <Input id="system-api-url" value={apiUrl} readOnly />
          </Form.Item>
          <div className="system-setting-group">
            <h2>设备在线状态</h2>
            <Form.Item name="onlineEnabled" label="启用在线查询" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item
              name="onlineServer"
              label="在线查询地址"
              extra="可填写 Hub 能访问的 ID 服务器地址和端口。留空使用上面的 ID 服务器。"
            >
              <Input placeholder="hbbs.internal:21116" disabled={!ready || saving || !onlineEnabled} autoComplete="off" />
            </Form.Item>
          </div>
          <Button type="primary" htmlType="submit" loading={saving || (!ready && !error)}>保存配置</Button>
        </Form>
      </section>
    </>
  )
}
