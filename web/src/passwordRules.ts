import type { FormRule } from 'antd'

export const newPasswordRules: FormRule[] = [
  { required: true, message: '请输入密码' },
  {
    validator: (_, value: string | undefined) => {
      if (value && (Array.from(value).length < 12 || new TextEncoder().encode(value).length > 256)) {
        return Promise.reject(new Error('请输入至少 12 个字符、最多 256 字节的密码'))
      }
      return Promise.resolve()
    },
  },
]

export function confirmPasswordRules(field: string): FormRule[] {
  return [
    { required: true, message: '请再次输入密码' },
    ({ getFieldValue }) => ({
      validator: (_, value: string | undefined) =>
        !value || value === getFieldValue(field)
          ? Promise.resolve()
          : Promise.reject(new Error('两次输入的密码不一致')),
    }),
  ]
}
