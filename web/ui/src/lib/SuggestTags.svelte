<script lang="ts">

  let {
    values = $bindable([]),
    items = [],
    allowAny = false,
    placeholder = 'input ...',
    size = 'medium'
  }: {
    values: string[],
    items: string[],
    allowAny?: boolean,
    placeholder?: string,
    size?: 'small' | 'medium' | 'large'
  } = $props();

  let inputValue = $state('');
  let suggestions = $derived(
    items
      .filter(item => 
        item.toLowerCase().includes(inputValue.toLowerCase()) &&
        !values.includes(item)
      )
      .slice(0, 10)
  );
  
  let selectedIndex = $state(-1);
  let showSuggestions = $state(false);

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      selectedIndex = Math.min(selectedIndex + 1, suggestions.length - 1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      selectedIndex = Math.max(selectedIndex - 1, -1);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      if (selectedIndex >= 0 && suggestions[selectedIndex]) {
        addTag(suggestions[selectedIndex]);
      } else if (allowAny && inputValue.trim()) {
        addTag(inputValue.trim());
      }
    }
  }

  function handlePaste(event: ClipboardEvent) {
    event.preventDefault();
    const pastedText = event.clipboardData?.getData('text') || '';

    // 使用逗号分割文本
    const tags = pastedText
      .split(',')
      .map(tag => tag.trim())
      .filter(tag => tag !== ''); // 跳过空字符串

    // 添加所有有效的标签
    tags.forEach(tag => {
      if (!values.includes(tag)) {
        values = [...values, tag];
      }
    });

    // 清空输入框
    inputValue = '';
    selectedIndex = -1;
    showSuggestions = false;
  }

  function addTag(tag: string) {
    if (!values.includes(tag)) {
      values = [...values, tag];
    }
    inputValue = '';
    selectedIndex = -1;
    showSuggestions = false;
  }

  function removeTag(tag: string) {
    values = values.filter(t => t !== tag);
  }

  function handleFocus() {
    showSuggestions = true;
  }

  function handleBlur() {
    // 延迟关闭建议列表，以允许点击建议项
    setTimeout(() => {
      showSuggestions = false;
      selectedIndex = -1;
    }, 200);
  }
</script>

<div class="relative w-full">
  <div class="flex flex-wrap items-center gap-1 px-2 bg-base-100 rounded-lg border border-base-300 focus-within:border-primary {size === 'small' ? 'py-1 min-h-8' : size === 'medium' ? 'py-1.5 min-h-10' : 'py-2 min-h-12 gap-2'}">
    {#each values as tag}
      <div class="badge badge-outline bg-primary-content/70 gap-1 pr-0 {size === 'small' ? 'text-xs' : size === 'medium' ? 'text-sm' : 'badge-lg'}">
        {tag}
        <button class="btn btn-ghost btn-xs px-1.5 py-0" onclick={() => removeTag(tag)}>×</button>
      </div>
    {/each}
    
    <input
      type="text"
      class="flex-1 min-w-[120px] bg-transparent border-none focus:outline-none {size === 'small' ? 'text-xs' : size === 'medium' ? 'text-sm' : ''}"
      bind:value={inputValue}
      onkeydown={handleKeydown}
      onfocus={handleFocus}
      onblur={handleBlur}
      onpaste={handlePaste}
      placeholder={placeholder}
    />
  </div>

  {#if showSuggestions && (suggestions.length > 0 || (allowAny && inputValue))}
    <div class="absolute z-50 w-full mt-1 bg-base-100 rounded-lg shadow-lg border border-base-300">
      <ul class="menu p-0 w-full">
        {#each suggestions as suggestion, index}
          <li class="w-full">
            <button
              class="px-4 py-2 hover:bg-base-200 {index === selectedIndex ? 'bg-base-200' : ''}"
              onmousedown={() => addTag(suggestion)}
              onmouseover={() => selectedIndex = index}
            >
              {suggestion}
            </button>
          </li>
        {/each}
        {#if allowAny && inputValue && !suggestions.includes(inputValue)}
          <li class="w-full">
            <button
              class="px-4 py-2 hover:bg-base-200 {selectedIndex === suggestions.length ? 'bg-base-200' : ''}"
              onmousedown={() => addTag(inputValue)}
              onmouseover={() => selectedIndex = suggestions.length}
            >
              {inputValue}
            </button>
          </li>
        {/if}
      </ul>
    </div>
  {/if}
</div> 