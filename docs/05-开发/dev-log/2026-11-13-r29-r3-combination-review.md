# R29 R3 隔离组合复核（2026-11-13）

- R28 已发布基线 d20714dd0d2801fea76a60a2ed6ed9059c43cec2。隔离组合提交 2b10be87bb1dace65db29d10b6a46dfdcd52ecff（Hydration）、c67c5df51c2bff680e60cbe2101a61e4246e27f6（DataGrid）、becb9f00377c036de79ef07e53913ddc82fe5db9（WindowHost）。
- 作者冻结 788350dd533d863e8507afa4625673f970f0408a、cee2c62fa881992587d5c1329425f62870934b03、4e46323c12dc0dad213f70bd99340c2bcec51939。DataGrid 的判据说明和开发日志收口见 cee2c62。
- 组合验证复现同名 Watch 共用发现记录、DataGrid 矛盾选择转换通过与缺几何字段异常、并发 Run 在所有权判定前报告成功。受影响包首轮回归另有 Windows CLI 修复文件时的共享冲突。候选保持隔离，原生和发布待续。
