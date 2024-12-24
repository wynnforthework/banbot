import type { FileNode, Tree, Node } from './types';
// 将文件列表转换为树形结构
export function buildTree(files: FileNode[], expand?: boolean): Tree {
  const tree: Tree = {
    children: []
  };
  
  // 先添加所有节点
  files.forEach(file => {
    const path = file.path;
    const isDir = path.endsWith('/');
    let name = '';
    if(isDir){
      name = path.substring(0, path.length - 1).split('/').pop() || '';
    }else{
      name = path.split('/').pop() || '';
    }
    
    tree[path] = {
      id: path,
      name: name,
      type: isDir ? 'container' : 'leaf',
      children: isDir ? [] : undefined,
      collapsed: expand === undefined ? isDir ? true : undefined : !expand
    };
  });

  // 构建父子关系
  files.forEach(file => {
    const path = file.path;
    const parts = path.substring(0, path.length - 1).split('/');
    
    if (parts.length > 1) {
      // 找到父目录
      const parentPath = parts.slice(0, -1).join('/') + '/';
      if (tree[parentPath]) {
        // 将当前节点添加到父节点的children中
        tree[parentPath].children = tree[parentPath].children || [];
        tree[parentPath].children.push(path);
      }
    } else {
      // 顶层节点
      tree.children.push(path);
    }
  });

  // 对每个目录的children进行排序
  const sortChildren = (children: string[]) => {
    return children.sort((a, b) => {
      const aIsDir = a.endsWith('/');
      const bIsDir = b.endsWith('/');
      
      // 如果一个是目录一个是文件，目录排在前面
      if (aIsDir !== bIsDir) {
        return aIsDir ? -1 : 1;
      }
      
      // 都是目录或都是文件，按名字字母顺序排序
      const aName = tree[a].name.toLowerCase();
      const bName = tree[b].name.toLowerCase();
      return aName.localeCompare(bName);
    });
  };

  // 对所有层级的目录进行排序
  const sortAllChildren = (node: Tree | Node) => {
    if (node.children) {
      node.children = sortChildren(node.children);
      // 递归排序子目录
      node.children.forEach(childId => {
        if (tree[childId] && tree[childId].children) {
          sortAllChildren(tree[childId]);
        }
      });
    }
  };

  // 从根节点开始排序
  sortAllChildren(tree);

  return tree;
}