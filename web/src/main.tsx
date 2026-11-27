import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App as AntApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import 'antd/dist/reset.css'
import { HubApp } from './HubApp'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: '#4e46c7',
          colorText: '#182038',
          colorTextSecondary: '#566078',
          colorTextPlaceholder: '#68728a',
          colorBorder: '#d8ddea',
          borderRadius: 8,
          controlHeight: 38,
          fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif',
        },
        components: {
          Table: {
            headerBg: '#f6f7fb',
            headerColor: '#39445e',
            borderColor: '#e5e9f3',
            rowHoverBg: '#f8f8fd',
          },
        },
      }}
    >
      <AntApp>
        <HubApp />
      </AntApp>
    </ConfigProvider>
  </StrictMode>,
)
