export default {
  app: { name: 'Prohibitorum' },
  common: { continue: '继续', cancel: '取消', signOut: '退出登录', copy: '复制', copied: '已复制', confirm: '确认', delete: '删除', rename: '重命名', save: '保存', create: '创建', revoke: '撤销', disable: '禁用', enable: '启用', loading: '加载中…', empty: '暂无内容' },
  login: { title: '登录', passkey: '使用通行密钥登录', password: '使用密码登录', or: '或', totp: '请输入动态验证码', signInWith: '使用 {name} 登录', username: '用户名', passwordLabel: '密码', submit: '提交', errorFallback: '登录失败，请重试' },
  consent: { title: '授权请求', requests: '「{app}」请求以下权限：', continueAs: '以 {account} 身份继续', approve: '允许', deny: '拒绝', errorFallback: '无法完成请求，请重试' },
  logout: { done: '您已退出登录', returnTo: '返回 {app}' },
  profile: { title: '个人资料', username: '用户名', displayName: '显示名称', role: '角色', logout: '退出登录' },
  sessions: { title: '会话', current: '当前', issuedAt: '登录时间', lastSeen: '最近活动', expiresAt: '过期时间', device: '设备', ip: 'IP', actions: '操作', revoke: '撤销' },
  error: { title: '出错了', generic: '发生了未知错误' },
  scopes: { openid: '基本身份', profile: '您的个人资料（姓名、昵称）', email: '您的邮箱地址', offline_access: '在您离线时持续访问', address: '您的地址', phone: '您的电话号码' },
  errors: { no_session: '请先登录', invalid_consent_ticket: '授权请求已失效，请重新发起登录', bad_credentials: '凭证无效', factor_locked: '尝试次数过多，请稍后再试', server_error: '服务器错误，请稍后再试' },
}
