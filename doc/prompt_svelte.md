
下面是svelte 4升级到svelte 5的改动：
```svelte
// 1. 隐式响应式改显式 `$state`
// Svelte 4: 隐式响应式 `let`
let count = 0;
// Svelte 5：显式 `$state`
let count = $state(0); // 需包裹响应式值

// 2. 派生状态与副作用"$:"改为`$derived` 和 `$effect`
// Svelte 4  自动追踪依赖
$: doubled = count * 2; // 派生状态
$: console.log('Changed'); // 副作用
// Svelte 5：显示使用`$derived` 和 `$effect`
let doubled = $derived(() => count * 2); // 派生状态
$effect(() => console.log('Changed')); // 副作用

// 3. 事件处理`on:click`改`onclick`(以及其他所有事件)
// Svelte 4: `on:` 指令 + 修饰符
<button on:click={handleClick}>Click</button>
//Svelte 5：原生事件名 + 函数包裹修饰符
<button onclick={handleClick}>Click</button>

// 4. 将`export let`替换为`$props()`语法
// Svelte 4: `export let` 声明属性
export let name = 'Guest';

//Svelte 5：解构 `$props()`
let {
  show=$bindable(false),
}: {
  show: boolean
} = $props();

// 5. 代码复用（片段）
// Svelte 4：插槽 `slot`
<Child>
  <p slot="content">Slot content</p>
</Child>

//Svelte 5：`{#snippet}` 定义可重用块
{#snippet content}
  <p>Reusable content</p>
{/snippet}
{@render content()}

// 6. 动态导入组件
<script>
  import A from './A.svelte';
  import B from './B.svelte';

  let Thing = $state();
  onMount(() => {
      Thing = A; // B
  })
</script>
// Svelte 4:
<svelte:component this={Thing} />
// Svelte 5：
<Thing />

// 7. page替换
将`import { page } from '$app/stores'`替换为`import { page } from '$app/state'`
将$page全部替换为page
```

你在生成的代码时应该遵循下面的要求：
1. 本项目使用svelte 5，你生成的代码应该严格符合上面最新svelte 5语法
2. 所有JavaScript代码转为typescript语法，确保任何地方都不是any或unknown对象。注意也把JavaScript函数的参数声明类型。
3. 项目目前使用paraglide-js的多语言支持，所有显示给用户的文本都应该在web/ui/messages/下的zh-CN.json和en-US.json中添加对应的解析，然后在svelte中使用下面方式替换，比如将<title>中的文本要对应为page_title等：
```typescript
import {m} from '$lib/paraglide/messages.js'
console.log(m.hello_world());
```
4. 在web/ui/messages/下的json文件中，不要使用嵌套多层，只使用一层key: value形式。
5. web/ui/messages/下的json多语言文件中，key的命名不要根据位置和业务模块决定，而是直接根据value文本确定，这样当其他地方使用相同value的key时，可以复用；比如"简单语法"对应为"simple_syntax"更合适。”全部“对应"all"，”搜索“对应"search"等。不要添加"btn","label", "text"等任何前缀，直接根据value文本值翻译并适当确定简短的key
