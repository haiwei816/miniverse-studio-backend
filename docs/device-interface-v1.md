# 实体伙伴接口预留 V1

当前交付只提供软件契约，不连接实体设备。玩家平台中的状态来自 API；不得将软件快照显示成硬件已连接。正式开发时需要单独完成设备身份、所有权验证、音频、重连、签名固件及吊销。

## 已可调用

以下接口都要求玩家平台 Bearer 会话，游戏限定 token 不能使用：

| 接口 | 当前行为 |
| --- | --- |
| `GET /v1/platform/devices/capabilities` | 返回 `protocol_version:1`、`implemented:false` 及预留消息字段 |
| `POST /v1/platform/devices/pair` | 返回 HTTP 501，错误码 `hardware_not_implemented`；不会伪造绑定记录 |
| `GET /v1/platform/devices/simulator` | 返回 `mode:software_simulator`、`hardware_connected:false`、该玩家的伙伴和当前世界快照 |

## 消息信封

机器可读契约见 [companion-device.v1.schema.json](../proto/companion-device.v1.schema.json)。此 schema 描述将来的事件格式，不代表已有可接受这些事件的公网或设备消息入口。

```json
{
  "protocol_version": 1,
  "event_id": "unique-event-id",
  "session_id": "active-companion-session",
  "context_epoch": 12,
  "sequence": 45,
  "expires_at": 2000000000,
  "type": "expression",
  "payload": { "name": "happy" }
}
```

后续实现要求：验证设备所属玩家和当前 session；同一 event_id 幂等；忽略过期、旧 epoch 和旧 sequence 的控制事件；重连先拉快照，不补放过期语音；切游戏、切账号、解绑时撤销旧会话。音频使用另行协商的流通道，不把完整录音塞进该事件格式。

设备凭证必须与玩家会话分离，不能让设备直接访问游戏库存或开发者控制台。配对码必须短期有效、单次使用，并验证设备所有权；仅知道 companion_id 不是绑定授权。真实设备接入前，本页面保持“未实现”状态。
