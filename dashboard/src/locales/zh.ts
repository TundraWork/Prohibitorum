export default {
  app: { name: 'Prohibitorum' },
  common: { continue: '继续', cancel: '取消', signOut: '退出登录' },
  login: { title: '登录', passkey: '使用通行密钥登录', password: '使用密码登录', or: '或', totp: '请输入动态验证码', signInWith: '使用 {name} 登录', username: '用户名', passwordLabel: '密码', submit: '提交', errorFallback: '登录失败，请重试' },
  consent: { title: '授权请求', requests: '「{app}」请求以下权限：', continueAs: '以 {account} 身份继续', approve: '允许', deny: '拒绝' },
  logout: { done: '您已退出登录', returnTo: '返回 {app}' },
  error: { title: '出错了', generic: '发生了未知错误' },
  scopes: { openid: '基本身份', profile: '您的个人资料（姓名、昵称）', email: '您的邮箱地址', offline_access: '在您离线时持续访问', address: '您的地址', phone: '您的电话号码' },
  errors: { no_session: '请先登录', invalid_consent_ticket: '授权请求已失效，请重新发起登录', bad_credentials: '凭证无效', factor_locked: '尝试次数过多，请稍后再试' },
}
