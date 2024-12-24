export type TreeNodeType = 'container' | 'leaf';

export interface Node {
	id: string;
	parentNodeId?: string;
	name: string;
	type: TreeNodeType;
	class?: string;
	collapsed?: boolean;
	disabled?: boolean;
	children?: string[];
}

export interface Tree {
	children: string[];
	[id: string]: TreeNode;
}

export interface MenuItem {
	icon?: string;
	key: string;
	text: string;
}

export interface MenuClickEvent {
	node: Node;
	key: string;
}


export interface FileNode {
	path: string;
	size?: number;
	stamp?: number;
}