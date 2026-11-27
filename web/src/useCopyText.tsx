import { App, Input } from 'antd'

export function useCopyText() {
  const { message, modal } = App.useApp()
  return async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value)
      void message.success(label + '已复制')
    } catch {
      modal.info({
        title: '手动复制' + label,
        content: (
          <Input.TextArea
            aria-label={label}
            readOnly
            value={value}
            autoSize={{ minRows: 2, maxRows: 8 }}
            onFocus={(event) => event.currentTarget.select()}
          />
        ),
        okText: '关闭',
      })
    }
  }
}
