import { CopyOutlined, DeleteOutlined, DesktopOutlined, EditOutlined, MoreOutlined, PlusOutlined, ReloadOutlined, TagsOutlined } from '@ant-design/icons'
import { Alert, App, Badge, Button, Dropdown, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag } from 'antd'
import { useEffect, useState } from 'react'
import { ApiError, request, type Book, type DeviceStatus, type Label, type OnlineStatus, type Peer } from './api'
import { TagsManager, colorHex } from './TagsManager'
import { useCopyText } from './useCopyText'

type AddressBookProps = {
  csrf: string
  onFailure: (error: unknown, mutation?: boolean) => string
}

const pageSize = 12
const emptyBook: Book = { data: [], total: 0, tags: [] }
const statusBadge = {
  online: { status: 'success', text: '在线' },
  offline: { status: 'default', text: '离线' },
  unknown: { status: 'warning', text: '未知' },
} as const

export function AddressBook({ csrf, onFailure }: AddressBookProps) {
  const [book, setBook] = useState<Book>(emptyBook)
  const [filter, setFilter] = useState('')
  const [selectedTag, setSelectedTag] = useState<string>()
  const [page, setPage] = useState(1)
  const [revision, setRevision] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [statusResult, setStatusResult] = useState<OnlineStatus | 'checking' | 'unavailable' | null>(null)
  const [editor, setEditor] = useState<{ peer?: Peer } | null>(null)
  const [tagsOpen, setTagsOpen] = useState(false)
  const { message } = App.useApp()
  const copyText = useCopyText()

  function reload() {
    setRevision((value) => value + 1)
  }

  useEffect(() => {
    const controller = new AbortController()
    const query = new URLSearchParams({ current: String(page), pageSize: String(pageSize), q: filter })
    if (selectedTag) query.set('tagId', selectedTag)
    setLoading(true)
    setError('')
    request<Book>('/address-book?' + query, { csrf, signal: controller.signal })
      .then((value) => {
        if (controller.signal.aborted) return
        if (selectedTag && !value.tags.some((tag) => tag.id === selectedTag)) {
          setSelectedTag(undefined)
          setPage(1)
          return
        }
        const lastPage = Math.max(1, Math.ceil(value.total / pageSize))
        if (page > lastPage) setPage(lastPage)
        setBook(value)
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) setError(onFailure(failure))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [csrf, filter, onFailure, page, revision, selectedTag])

  useEffect(() => {
    if (book.data.length === 0) {
      setStatusResult(null)
      return
    }
    const controller = new AbortController()
    const query = new URLSearchParams()
    for (const { id } of book.data) query.append('id', id)
    setStatusResult('checking')
    request<OnlineStatus>('/address-book/online-status?' + query, { csrf, signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) setStatusResult(value)
      })
      .catch((failure: unknown) => {
        if (controller.signal.aborted) return
        if (failure instanceof ApiError && failure.status === 401) onFailure(failure)
        setStatusResult('unavailable')
      })
    return () => controller.abort()
  }, [book.data, csrf, onFailure])

  async function remove(peer: Peer) {
    try {
      await request('/address-book/peers', { method: 'DELETE', csrf, body: [peer.id] })
      void message.success('设备已移除')
      reload()
    } catch (failure) {
      setError(onFailure(failure, true))
    }
  }

  const colors = new Map(book.tags.map((tag) => [tag.name, colorHex(tag.color)]))
  const options = book.tags.map(({ id, name }) => ({ label: name, value: id }))
  const statuses = typeof statusResult === 'object' ? statusResult?.statuses : undefined

  return (
    <>
      <div className="page-heading">
        <div>
          <h1>个人通讯录</h1>
          <p>在网页和已登录的 RustDesk 客户端之间同步设备。</p>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditor({})}>添加设备</Button>
      </div>

      <section className="panel" aria-label="设备列表">
        <div className="book-tools">
          <div className="book-filters">
            <Input.Search
              className="book-search"
              aria-label="按设备 ID 或别名搜索"
              placeholder="按设备 ID 或别名搜索"
              allowClear
              onSearch={(value) => {
                setPage(1)
                setFilter(value.trim())
              }}
            />
            <Select
              className="tag-filter"
              aria-label="按标签筛选"
              placeholder="全部标签"
              allowClear
              value={selectedTag}
              options={options}
              onChange={(value: string | undefined) => {
                setSelectedTag(value)
                setPage(1)
              }}
            />
          </div>
          <Space wrap>
            <Button icon={<TagsOutlined />} onClick={() => setTagsOpen(true)}>管理标签</Button>
            <Button icon={<ReloadOutlined />} onClick={reload}>刷新</Button>
          </Space>
        </div>
        {error && <Alert className="table-alert" title={error} type="error" showIcon />}
        {book.data.length > 0 && <StatusNote result={statusResult} />}
        <Table<Peer>
          rowKey="id"
          dataSource={book.data}
          loading={loading}
          locale={{ emptyText: filter || selectedTag ? '未找到符合条件的设备' : '通讯录为空，点击「添加设备」开始' }}
          scroll={{ x: 750 }}
          pagination={{
            current: page,
            pageSize,
            total: book.total,
            showSizeChanger: false,
            showTotal: (total) => '共 ' + total + ' 台设备',
            onChange: setPage,
          }}
          columns={[
            {
              title: '设备 / RustDesk ID',
              width: '27%',
              render: (_, peer) => (
                <div className="device-name">
                  <strong>{peer.alias || peer.id}</strong>
                  {peer.alias && <span className="device-id">{peer.id}</span>}
                </div>
              ),
            },
            {
              title: '在线状态',
              width: 100,
              render: (_, peer) => {
                const status: DeviceStatus = statuses && Object.hasOwn(statuses, peer.id)
                  ? statuses[peer.id] ?? 'unknown'
                  : 'unknown'
                return <Badge {...statusBadge[status]} />
              },
            },
            {
              title: '标签',
              width: '18%',
              render: (_, peer) => peer.tags.length === 0 ? '—' : (
                <Space size={[4, 4]} wrap>
                  {peer.tags.map((name) => (
                    <Tag key={name} className="device-tag">
                      <span className="tag-dot" style={{ backgroundColor: colors.get(name) }} />{name}
                    </Tag>
                  ))}
                </Space>
              ),
            },
            { title: '备注', dataIndex: 'note', render: (note: string) => <span className="device-note">{note || '—'}</span> },
            {
              title: '操作', width: 220,
              render: (_, peer) => {
                const name = peer.alias || peer.id
                const copyLabel = peer.connection ? '连接地址' : '设备 ID'
                const copyTarget = () => void copyText(peer.connection?.target ?? peer.id, copyLabel)
                return (
                  <Space size={4}>
                    {peer.connection?.url ? (
                      <>
                        <Button
                          type="link"
                          size="small"
                          icon={<DesktopOutlined />}
                          aria-label={'连接 ' + name}
                          href={peer.connection.url}
                          onClick={() => void message.info('已请求打开 RustDesk。未打开时可复制连接地址。')}
                        >
                          连接
                        </Button>
                        <Dropdown
                          trigger={['click']}
                          menu={{ items: [{ key: 'copy', label: '复制连接地址', icon: <CopyOutlined />, onClick: copyTarget }] }}
                        >
                          <Button size="small" type="text" icon={<MoreOutlined />} aria-label={'更多操作 ' + name} />
                        </Dropdown>
                      </>
                    ) : (
                      <Button size="small" type="link" icon={<CopyOutlined />} onClick={copyTarget}>
                        复制{copyLabel}
                      </Button>
                    )}
                    <Button size="small" type="text" icon={<EditOutlined />} onClick={() => setEditor({ peer })}>编辑</Button>
                    <Popconfirm
                      title="移除这台设备？"
                      description="各端下次刷新后将同步移除。"
                      onConfirm={() => remove(peer)}
                      okText="移除"
                      okButtonProps={{ danger: true }}
                    >
                      <Button size="small" type="text" danger icon={<DeleteOutlined />} aria-label={'移除 ' + name} />
                    </Popconfirm>
                  </Space>
                )
              },
            },
          ]}
        />
      </section>
      {editor && (
        <PeerEditor
          peer={editor.peer}
          labels={book.tags}
          csrf={csrf}
          onFailure={onFailure}
          onClose={() => setEditor(null)}
          onSaved={(feedback) => {
            void message.success(feedback)
            setEditor(null)
            reload()
          }}
        />
      )}
      {tagsOpen && (
        <TagsManager
          labels={book.tags}
          csrf={csrf}
          onFailure={onFailure}
          onClose={() => setTagsOpen(false)}
          onChange={reload}
        />
      )}
    </>
  )
}

function StatusNote({ result }: { result: OnlineStatus | 'checking' | 'unavailable' | null }) {
  let content
  if (result === 'checking') {
    content = '正在查询在线状态…'
  } else if (result === 'unavailable') {
    content = '在线状态暂不可查询'
  } else if (result && !result.configured) {
    content = '在线查询未启用'
  } else if (result?.checkedAt) {
    const time = new Date(result.checkedAt)
    const failed = Object.values(result.statuses).every((status) => status === 'unknown')
    content = (
      <>
        {failed ? '状态查询失败于 ' : '状态查询于 '}
        <time dateTime={result.checkedAt} title={time.toLocaleString()}>{time.toLocaleTimeString('zh-CN', { hour12: false })}</time>
      </>
    )
  }
  return <div className="book-status" role="status">{content}</div>
}

type EditorProps = AddressBookProps & {
  peer?: Peer
  labels: Label[]
  onClose: () => void
  onSaved: (message: string) => void
}

function PeerEditor({ peer, labels, csrf, onFailure, onClose, onSaved }: EditorProps) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function save(values: Peer) {
    setSaving(true)
    setError('')
    try {
      if (!peer) {
        const result = await request<{ created: boolean }>('/address-book/peers', {
          method: 'POST',
          csrf,
          body: { ...values, id: values.id.trim() },
        })
        onSaved(result.created ? '设备已添加' : '该设备已在通讯录中')
        return
      }

      const updates: Partial<Peer> = {}
      if (values.alias !== peer.alias) updates.alias = values.alias
      if (values.note !== peer.note) updates.note = values.note
      const names = values.tags ?? []
      if (JSON.stringify([...names].sort()) !== JSON.stringify([...peer.tags].sort())) updates.tags = names
      if (Object.keys(updates).length > 0) {
        await request('/address-book/peers', { method: 'PATCH', csrf, body: { id: peer.id, ...updates } })
        onSaved('设备已更新')
      } else {
        onClose()
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
      title={peer ? '编辑设备' : '添加设备'}
      footer={null}
      onCancel={onClose}
      closable={!saving}
      mask={{ closable: !saving }}
      keyboard={!saving}
    >
      {error && <Alert type="error" title={error} showIcon className="form-alert" />}
      <Form<Peer>
        layout="vertical"
        initialValues={peer ?? { id: '', alias: '', note: '', tags: [] }}
        onFinish={save}
        disabled={saving}
      >
        <Form.Item name="id" label="RustDesk ID" rules={[{ required: true, whitespace: true, message: '请输入设备 ID' }]}>
          <Input readOnly={Boolean(peer)} autoComplete="off" />
        </Form.Item>
        <Form.Item name="alias" label="别名">
          <Input placeholder="留空时显示设备 ID" />
        </Form.Item>
        <Form.Item name="tags" label="标签">
          <Select
            mode="multiple"
            placeholder="选择已有标签"
            options={labels.map(({ name }) => ({ label: name, value: name }))}
          />
        </Form.Item>
        <Form.Item name="note" label="备注">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} />
        </Form.Item>
        <div className="dialog-actions">
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" htmlType="submit" loading={saving}>保存</Button>
        </div>
      </Form>
    </Modal>
  )
}
