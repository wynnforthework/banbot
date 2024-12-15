/**交易账户 */
export interface BotAccount{
  url: string
  name: string
  account: string
  role: string
  token: string
  running?: boolean
  dayDoneNum?: number
  dayDonePft?: number
  dayOpenNum?: number
  dayOpenPft?: number
}

/** 
登录信息，对应多个交易账户和不同权限
*/
export interface BotTicket{
  url: string
  user_name: string
  password: string
  name?: string
  token?: string
  accounts?: Record<string, string>
}