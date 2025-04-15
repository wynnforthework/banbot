# BanBot UI
这是交易机器人banbot的前端部分；K线使用Klinecharts；基于sveltekit架构；ui使用daisyUI+tailwind

## 开发笔记
**国际化**  
使用inlang的国际化[插件](https://inlang.com/m/gerre34r/library-inlang-paraglideJs/sveltekit)
1. 在 project.inlang/settings.json 中添加语言标签，并在 messages 文件夹下添加语言文件。
2. 在需要使用语言标签的地方，使用:
```typescript
import * as m from '$lib/paraglide/messages.js'
m.hello()
m['hello']()
```

**注册新指标**  
在 src/lib/coms.ts 中添加新指标的字段和默认参数。

**编译为静态资源**  
sveltekit支持编译静态资源，只需将svelte.config.js中的`adapter-auto`改为`adapter-static`，然后执行`npm run build`，即可在dist目录下生成静态资源。

**云端指标**  
本项目原生支持云端指标加载和显示，后端需提供`/kline/all_inds`和`/kline/calc_ind`接口，具体参数请参考`src/lib/indicators/cloudInds.ts`

## Developing
初始化：`npm install` (or `pnpm install` or `yarn`):

```bash
npx @inlang/paraglide-js compile --project ./project.inlang --outdir ./src/lib/paraglide

npm run dev
```

## Building
已创建github workflow，在修改前端资源文件后自动执行编译，下载dist.zip即可。 
```bash
npm run build

npm run preview
```
