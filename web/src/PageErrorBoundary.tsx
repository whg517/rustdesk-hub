import { Alert, Button } from 'antd'
import { Component, type ReactNode } from 'react'

type Props = { children: ReactNode }
type State = { failed: boolean }

export class PageErrorBoundary extends Component<Props, State> {
  state: State = { failed: false }

  static getDerivedStateFromError(): State {
    return { failed: true }
  }

  render() {
    if (this.state.failed) {
      return (
        <Alert
          type="error"
          showIcon
          title="页面加载失败"
          action={<Button onClick={() => window.location.reload()}>重新加载</Button>}
        />
      )
    }
    return this.props.children
  }
}
