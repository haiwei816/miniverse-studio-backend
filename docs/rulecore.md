# C++20 RuleCore

默认 Go + MySQL 即可处理存档、活动、邮件与普通排行榜。需要规则校验时启用 Compose 的 rules profile；C++ 端口不映射到主机。Go 每次按需通过 gRPC 调用 Replay，超时两秒；不可用时拒绝验证，普通 API 保持可用。

card-v1 规则使用固定宽度整数、显式 xorshift32 和按小端字节计算的 FNV1a 状态哈希，无浮点数、时钟或未定义溢出。动作 0 为防御、1/2 为攻击。玩家初始生命 20，敌人 30；非法动作、错误规则版本、零种子、空/超长回放和终局后动作均拒绝。

固定验收向量：version=card-v1，seed=123，actions=[2,2,2,2,2,2] → 胜利、score=36、state_hash=8660768014306689972。C++ 单测重复 1000 次并检查固定结果；HTTP 测试验证 Go→gRPC→C++ 完整链路。

card-game 的 verified 榜只接受回放验证入口，普通成绩上报返回 replay_required。客户端声称的分数与计算结果不符则拒绝。回放、分数和幂等记录同一事务提交。

确定性回放验证证明“给定种子和动作符合规则”，不证明客户端实际经历了这一局。当前种子由客户端提供，因此不适合高价值奖励或严肃竞技；正式竞争应增加服务器签发的挑战种子、对局上下文和重放防护。内部 gRPC 当前使用 Compose 网络明文连接；跨主机部署需 TLS 与服务身份。C++ 镜像保留编译工具以方便复现，生产镜像瘦身属于后续部署工作。Lua 为可选扩展，未加入本 MVP。

协议生成参考 [gRPC Go 官方指南](https://grpc.io/docs/languages/go/quickstart/) 和 [C++ 官方指南](https://grpc.io/docs/languages/cpp/basics/)。生成代码已纳入工程；重新生成：

```sh
docker build -t miniverse-rulecore:local -f rulecore/Dockerfile .
docker run --rm --user root --entrypoint sh -v "$PWD:/repo" miniverse-rulecore:local -c 'cd /repo && protoc -I proto --go_out=backend --go_opt=module=miniverse/backend --go-grpc_out=backend --go-grpc_opt=module=miniverse/backend proto/rule.proto'
```
