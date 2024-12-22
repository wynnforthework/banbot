<script lang="ts">
  import * as m from '../paraglide/messages.js'
  import CodeMirror from '$lib/dev/CodeMirror.svelte';
  import { oneDark } from '@codemirror/theme-one-dark';
  import type { Extension } from '@codemirror/state';
  import { onMount } from 'svelte';

  const cfgYml = `name: local  # ${m.cfg_name()}
env: prod  # ${m.cfg_env()}
leverage: 2  # ${m.cfg_leverage()}
limit_vol_secs: 5  # ${m.cfg_limit_vol_secs()}
put_limit_secs: 120  # ${m.cfg_put_limit_secs()}
market_type: spot  # ${m.cfg_market_type()}
contract_type: swap  # ${m.cfg_contract_type()}
odbook_ttl: 1000  # ${m.cfg_odbook_ttl()}
concur_num: 2  # ${m.cfg_concur_num()}
order_type: market  # ${m.cfg_order_type()}
stop_enter_bars: 20  # ${m.cfg_stop_enter_bars()}
prefire: 0  # ${m.cfg_prefire()}
margin_add_rate: 0.66  # ${m.cfg_margin_add_rate()}
stake_amount: 15  # ${m.cfg_stake_amount()}
stake_pct: 50  # ${m.cfg_stake_pct()}
max_stake_amt: 5000  # ${m.cfg_max_stake_amount()}
open_vol_rate: 1  # ${m.cfg_open_vol_rate()}
min_open_rate: 0.5  # ${m.cfg_min_open_rate()}
bt_net_cost: 15  # ${m.cfg_bt_net_cost()}
wallet_amounts:  # ${m.cfg_wallet_amounts()}
  USDT: 10000
stake_currency: [USDT, TUSD]  # ${m.cfg_stake_currency()}
fatal_stop:  # ${m.cfg_fatal_stop()}
  '1440': 0.1  # ${m.cfg_fatal_stop_1440()}
  '180': 0.2  # ${m.cfg_fatal_stop_180()}
  '30': 0.3  # ${m.cfg_fatal_stop_30()}
fatal_stop_hours: 8  # ${m.cfg_fatal_stop_hours()}
timerange: "20230701-20230808"  # ${m.cfg_timerange()}
run_timeframes: [5m]  # ${m.cfg_run_timeframes()}
run_policy:  # ${m.cfg_run_policy()}
  - name: Demo  # ${m.cfg_run_policy_name()}
    run_timeframes: [5m]  # ${m.cfg_run_policy_timeframes()}
    filters:  # ${m.cfg_run_policy_filters()}
    - name: OffsetFilter
      offset: 10
      limit: 30
    max_pair: 999  # ${m.cfg_run_policy_max_pair()}
    max_open: 10  # ${m.cfg_run_policy_max_open()}
    dirt: any  # ${m.cfg_run_policy_dirt()}
    pairs: [BTC/USDT:USDT]
    params: {atr: 15}
    pair_params:
      BTC/USDT:USDT: {atr:14}
    strat_perf:
      enable: false
strat_perf:
  enable: false  # ${m.cfg_strat_perf_enable()}
  min_od_num: 5  # ${m.cfg_strat_perf_min_od_num()}
  max_od_num: 30  # ${m.cfg_strat_perf_max_od_num()}
  min_job_num: 10  # ${m.cfg_strat_perf_min_job_num()}
  mid_weight: 0.2  # ${m.cfg_strat_perf_mid_weight()}
  bad_weight: 0.1  # ${m.cfg_strat_perf_bad_weight()}
pairs:  # ${m.cfg_pairs()}
- SOL/USDT:USDT
- UNFI/USDT:USDT
- SFP/USDT:USDT
pairmgr:
  cron: '25 1 0 */2 * *' # ${m.cfg_pairmgr_corn()}
  offset: 0  # ${m.cfg_pairmgr_offset()}
  limit: 999  # ${m.cfg_pairmgr_limit()}
  force_filters: false # ${m.cfg_pairmgr_force_filters()}
pairlists:  # ${m.cfg_pairlists()}
  - name: VolumePairList  # ${m.cfg_pairlists_vol()}
    limit: 100  # ${m.cfg_pairlists_limit100()}
    min_value: 100000  # ${m.cfg_pairlists_min_value()}
    refresh_secs: 7200  # ${m.cfg_pairlists_refresh_secs()}
    back_timeframe: 1d  # ${m.cfg_pairlists_back_timeframe()}
    back_period: 1  # ${m.cfg_pairlists_back_period()}
  - name: PriceFilter
    max_unit_value: 100  # ${m.cfg_pairlists_max_unit_value()}
    precision: 0.0015  # ${m.cfg_price_precision()}
    min: 0.001  # ${m.cfg_min_price()}
    max: 100000  # ${m.cfg_max_price()}
  - name: RateOfChangeFilter
    back_days: 5  # ${m.cfg_back_days()}
    min: 0.03  # ${m.cfg_roc_min()}
    max: 10  # ${m.cfg_roc_max()}
    refresh_period: 1440  # ${m.cfg_refresh_period()}
  - name: SpreadFilter  # ${m.cfg_spread_filter()}
    max_ratio: 0.005  # ${m.cfg_spread_max()}
  - name: CorrelationFilter  # ${m.cfg_correlation()}
    min: -1  # ${m.cfg_correlation_val()}
    max: 1  # ${m.cfg_correlation_val()}
    timeframe: 5m  # ${m.cfg_correlation_tf()}
    back_num: 70  # ${m.cfg_correlation_back()}
    top_n: 50  # ${m.cfg_correlation_topn()}
  - name: VolatilityFilter  # ${m.cfg_volatility()}
    back_days: 10  # ${m.cfg_back_days()}
    max: 1  # ${m.cfg_volatility_max()}
    min: 0.05  # ${m.cfg_volatility_min()}
  - name: AgeFilter  # ${m.cfg_pairlists_age()}
    min: 5
  - name: OffsetFilter  # ${m.cfg_pairlists_offset()}
    offset: 10
    limit: 30
  - name: ShuffleFilter  # ${m.cfg_pairlists_shuffle()}
    seed: 42
exchange:
  name: binance  # ${m.cfg_exg_name()}
  binance:
    account_prods:  # ${m.cfg_acc_prod()}
      user1: # ${m.cfg_acc_name()}
        api_key: xxx
        api_secret: bbb
        max_stake_amt: 1000  # ${m.cfg_acc_max_stake()}
        stake_rate: 1  # ${m.cfg_acc_stake_rate()}
        leverage: 0  # ${m.cfg_acc_lvg()}
      user2: # ${m.cfg_acc_another()}
        api_key: xxx
        api_secret: bbb
    account_tests:  # ${m.cfg_acc_test()}
      default:
        api_key: xxx
        api_secret: bbb
    options:  # ${m.cfg_exg_options()}
`
  let theme: Extension | null = $state(oneDark);
  let editor: CodeMirror | null = $state(null);

  onMount(() => {
    if(editor){
      editor.setValue('config.yml', cfgYml);
    }else{
      setTimeout(() => {
        editor?.setValue('config.yml', cfgYml);
      }, 10);
    }
  });
</script>

<div class="flex justify-between items-center mb-4">
  <h3 class="text-lg font-semibold">{m.full_config()}</h3>
  <label for="config-drawer" class="btn btn-sm btn-circle">✕</label>
</div>
<a href="https://bot.banbox.top/en/configuration.html" target="_blank" class="link">{m.doc_config()}</a>
<CodeMirror bind:this={editor} {theme} editable={false}/>