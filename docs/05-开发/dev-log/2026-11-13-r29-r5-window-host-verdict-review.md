# R29 R5 宿主终态独立复核

- 修复提交 1e52179b19f295080e7b7ea2c2c755a839a25837。认领 Gio Main 期间，ExitAll 只登记退出请求。Run 完成认领后发布宿主终态，拒绝原因由 Wait 和 Run 一致交出。
- 在组合提交 92bc1975ac4d04981c807ddbeee7ac4f6383eb63 上，原探针通过，但增加拒绝原因断言后实测 Rejected=false、Err=nil。修复后用同一调度门闩覆盖终态的发布顺序和拒绝原因。
