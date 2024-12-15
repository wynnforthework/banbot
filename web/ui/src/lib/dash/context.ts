import type { BotTicket, BotAccount } from './types';

export class DashCtx {
  accounts: BotAccount[]
  clickPage: number

  constructor() {
    this.accounts = []
    this.clickPage = 0
  }
}

export class DashSave {
  tickets: BotTicket[];
  current: string; // url_account
  
  constructor() {
    this.tickets = [];
    this.current = '';
  }
}