# PIT-020：Windows junction不能用symlink测试代替

## 现象与根因

输出路径的普通目录symlink反例全部通过，但独立审核用`mklink /J`创建junction，在其下生成不存在的子目录，CLI仍rc0并在module外落盘。本机Go1.26.1的Lstat将junction叶子标为ModeIrregular；该形状不满足ModeSymlink，EvalSymlinks可能保留词法路径。检查“已做过symlink测试”不能推出junction已覆盖。

## 处理与预防

对实际支持的Windows路径形态分别验证：普通目录、目录symlink、junction、已有输出和缺失后代目录；同时保留模块内合法链接正向对照。对无法证实安全的异常/reparse祖先拒绝写入，覆盖链接解析后的祖先。不要为了全绿把真实junction测试跳过，也不要把词法module根当真实存储位置。

数据源是本批独立审核的真实CLI外写反例与Go路径语义对照，非模型推演；作者保留首修提交，另以最小补丁修复后再交叉复核。见[BUG-023](../07-Bug/BUG-023-生成输出与Manifest路径可越出module.md)及[本批验证记录](../06-测试/2026-10-31-并行开发验证记录.md)。不扩大为已抵御恶意并发目录替换的结论。
