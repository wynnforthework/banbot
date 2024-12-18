<script lang="ts">
	import type { Node, Tree, MenuItem, MenuClickEvent } from './types';
  import Self from './TreeNode.svelte';
  import Icon from '$lib/Icon.svelte';

  let {tree, node, click,
    collapseControlled = false, 
    treeNodeClass = '', 
    childrenContainerClass = '', 
    childPaddingLeft = '1rem',
		active = '',
		activeMenuId = '',
    onMenu,
    onMenuClick
	}: {
    tree: Tree, 
    node: Node,
    collapseControlled?: boolean,
    treeNodeClass?: string,
    childrenContainerClass?: string,
    childPaddingLeft?: string,
		active?: string,
		activeMenuId?: string,
    click: (event: {node: Node, collapsed: boolean}) => void,
    onMenu?: (node: Node) => MenuItem[],
    onMenuClick?: (event: MenuClickEvent) => void,
  } = $props();

	let collapsed = $state(!node || !!node.collapsed);

	let menuItems: MenuItem[] = $state([]);
	let showMenu = $state(false);
	let menuX = $state(0);
	let menuY = $state(0);

	$effect(() => {
		if (collapseControlled) {
				collapsed = !!node.collapsed;
		}
	});

	const nodeClz = $derived(`tree-view_node ${node.type} ${
		collapsed ? 'tree-view_node-collapsed' : ''
	} ${treeNodeClass} ${node.disabled ? 'disabled' : ''}`);

	const arrowClz = $derived(`tree-view_arrow ${collapsed ? 'tree-view_arrow-collapsed' : ''}`);

	$effect(() => {
		if (active && active !== node.id || activeMenuId && activeMenuId !== node.id) {
			setTimeout(() => {
				if(showMenu) {
					showMenu = false;
				}
			}, 0);
		}
	});

	function handleClick(event: MouseEvent) {
		event.stopPropagation();
		
		if (isNodeDisabled(node, tree)) return;
		if (node.type === 'container' && !collapseControlled) {
			collapsed = !collapsed;
		}
		click({ node, collapsed });
	}

	function isNodeDisabled(node: Node, tree: Tree) {
		let parentNode = tree[node.id];
		while (parentNode && !parentNode.disabled) {
			parentNode = tree[parentNode.parentNodeId];
		}

		if (!parentNode) return false;
		else return parentNode.disabled;
	}

	function handleContextMenu(event: MouseEvent) {
		event.preventDefault();
		event.stopPropagation();
		
		if (isNodeDisabled(node, tree) || !onMenu) return;
		
		menuItems = onMenu(node);
		if (menuItems.length) {
			menuX = event.clientX;
			menuY = event.clientY;
			showMenu = true;
		}
	}

	function handleMenuClick(event: MouseEvent, key: string) {
		event.stopPropagation();
		showMenu = false;
		onMenuClick?.({ node, key });
	}

	function handleWindowClick() {
		showMenu = false;
	}
</script>

<svelte:window on:click={handleWindowClick} />

<div class={nodeClz} onclick={handleClick} oncontextmenu={handleContextMenu}>
	<div class="tree-view_content" class:active={node.id == active}>
		{#if node.type === 'container' && collapsed}
			<div class={arrowClz}><Icon name="chevron-right" /></div>
		{:else if node.type === 'container' && !collapsed}
			<div class={arrowClz}><Icon name="chevron-down" /></div>
		{:else if node.type === 'leaf'}
			<div class={arrowClz}><Icon name="(empty)" /></div>
		{/if}
		<div class="tree-view_name">{node.name}</div>
	</div>
	{#if !collapsed && node.type === 'container' && node.children?.length}
		<div
			class={'tree-view_children ' + childrenContainerClass}
			style:margin-left={childPaddingLeft}
		>
			{#each node.children as childId (childId)}
				{#if tree[childId]}
					<div>
						<Self {tree} {active} {activeMenuId} node={tree[childId]} {treeNodeClass} {childrenContainerClass} {click}
							onMenu={onMenu}
							onMenuClick={onMenuClick}
						/>
					</div>
				{/if}
			{/each}
		</div>
	{/if}
</div>

{#if showMenu}
	<div 
		class="context-menu px-4" 
		style="left: {menuX}px; top: {menuY}px"
	>
		{#each menuItems as item}
			<div 
				class="menu-item" 
				onclick={(e) => handleMenuClick(e, item.key)}
			>
				{#if item.icon}
					<div class="menu-icon">
						<Icon name={item.icon} />
					</div>
				{/if}
				<span>{item.text}</span>
			</div>
		{/each}
	</div>
{/if}

<style>
	.tree-view_node {
			cursor: pointer;
			width: 100%;
	}

	.tree-view_node > .tree-view_content:hover {
			background-color: rgba(0, 0, 0, 0.05);
	}

	.tree-view_node > .tree-view_content.active {
			background-color: rgba(0, 0, 0, 0.08);
	}

	.tree-view_content {
			align-items: center;
			display: flex;
			gap: 0.3rem;
			padding: 0.2rem 0;
			width: 100%;
	}

	.tree-view_arrow {
			align-items: center;
			display: flex;
			width: 1rem;
			height: 1rem;
	}

	.context-menu {
			position: fixed;
			background: white;
			border: 1px solid #ddd;
			border-radius: 4px;
			padding: 4px 4px;
			min-width: 120px;
			box-shadow: 0 2px 8px rgba(0,0,0,0.15);
			z-index: 1000;
	}

	.menu-icon {
			width: 1rem;
			height: 1rem;
			display: flex;
			align-items: center;
			justify-content: center;
	}

	.menu-item {
			padding: 6px 12px;
			display: flex;
			align-items: center;
			gap: 8px;
			cursor: pointer;
	}

	.menu-item:hover {
			background-color: rgba(0,0,0,0.05);
	}
</style>