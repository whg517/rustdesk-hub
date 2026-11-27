import { App, Alert, Button, Form, Input, Modal, Popconfirm, Space, Table } from 'antd'
import { useState } from 'react'
import { request, type Label } from './api'

const defaultColor = '#4e46c7'

export function colorHex(color: number) {
  return '#' + (color & 0xffffff).toString(16).padStart(6, '0')
}

type TagForm = { name: string; color: string }

type Props = {
  labels: Label[]
  csrf: string
  onFailure: (error: unknown, mutation?: boolean) => string
  onChange: () => void
  onClose: () => void
}

export function TagsManager({ labels, csrf, onFailure, onChange, onClose }: Props) {
  const [form] = Form.useForm<TagForm>()
  const [editing, setEditing] = useState<Label>()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const { message } = App.useApp()

  function selectLabel(item?: Label) {
    setEditing(item)
    setError('')
    form.setFieldsValue({ name: item?.name ?? '', color: item ? colorHex(item.color) : defaultColor })
  }

  async function save(value: TagForm) {
    const name = value.name.trim()
    const color = 0xff000000 + Number.parseInt(value.color.slice(1), 16)
    setBusy(true)
    setError('')
    try {
      if (editing) {
        const changes: { newName?: string; color?: number } = {}
        if (value.name !== editing.name && name !== editing.name) changes.newName = name
        if (value.color !== colorHex(editing.color)) changes.color = color
        if (Object.keys(changes).length > 0) {
          await request('/address-book/tags', {
            method: 'PATCH',
            csrf,
            body: { tagId: editing.id, ...changes },
          })
          void message.success('标签已更新')
        }
      } else {
        const result = await request<{ created: boolean }>('/address-book/tags', {
          method: 'POST',
          csrf,
          body: { name, color },
        })
        void message.success(result.created ? '标签已添加' : '该标签已存在')
      }
      selectLabel()
      onChange()
    } catch (failure) {
      setError(onFailure(failure, true))
    } finally {
      setBusy(false)
    }
  }

  async function remove(label: Label) {
    setBusy(true)
    setError('')
    try {
      await request('/address-book/tags', { method: 'DELETE', csrf, body: { tagId: label.id } })
      if (label.id === editing?.id) selectLabel()
      void message.success('标签已移除')
      onChange()
    } catch (failure) {
      setError(onFailure(failure, true))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title="管理标签"
      footer={null}
      onCancel={onClose}
      closable={!busy}
      mask={{ closable: !busy }}
      keyboard={!busy}
    >
      {error && <Alert type="error" title={error} showIcon className="form-alert" />}
      <Form<TagForm> form={form} layout="vertical" initialValues={{ name: '', color: defaultColor }} onFinish={save} disabled={busy}>
        <div className="label-tools">
          <Form.Item className="tag-name-field" name="name" label="标签名称" rules={[{ required: true, whitespace: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="color" label="颜色">
            <input className="color-picker" type="color" />
          </Form.Item>
          <Form.Item label=" ">
            <Space size={4}>
              {editing && <Button onClick={() => selectLabel()}>取消</Button>}
              <Button type="primary" htmlType="submit" loading={busy}>{editing ? '保存修改' : '添加标签'}</Button>
            </Space>
          </Form.Item>
        </div>
      </Form>
      <Table<Label>
        size="small"
        rowKey="id"
        dataSource={labels}
        locale={{ emptyText: '暂无标签' }}
        pagination={labels.length <= 6 ? false : { pageSize: 6, hideOnSinglePage: true }}
        columns={[
          { title: '标签', render: (_, label) => (
            <span><span className="tag-dot" style={{ backgroundColor: colorHex(label.color) }} />{label.name}</span>
          ) },
          { title: '操作', width: 150, render: (_, label) => (
            <Space size={4}>
              <Button type="text" size="small" disabled={busy} onClick={() => selectLabel(label)}>编辑</Button>
              <Popconfirm title="移除该标签？" description="通讯录中的设备条目会保留。" onConfirm={() => remove(label)} okText="移除">
                <Button type="text" size="small" danger disabled={busy}>移除</Button>
              </Popconfirm>
            </Space>
          ) },
        ]}
      />
    </Modal>
  )
}
