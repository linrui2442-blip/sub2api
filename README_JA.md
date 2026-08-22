# Sub2API Personal Private Edition

Windows を第一にした、オーナーと少数の信頼できるメンバー向けのプライベート LLM
ゲートウェイです。公開 SaaS やチャットアプリではなく、長時間稼働する AI Agent の
基盤です。

- GPT/OpenAI、Gemini、および将来の Claude/Anthropic Provider 拡張
- Provider OAuth、トークン保存、自動更新
- Account Pool、health check、quota、scheduler、cooldown、failover
- OpenAI 互換 API Gateway、API Key、Usage、Audit
- SQLite とプロセス内ローカルキャッシュ。Docker、PostgreSQL、Redis、WSL は不要

公開登録、ソーシャルログイン、テナント、課金、サブスクリプション、紹介、クラウド
デプロイは Personal Edition には含まれません。既定では `127.0.0.1` で待ち受けます。

Windows リリースを展開し `sub2api-personal.exe` を実行すると、ローカルのセットアップ
画面が開きます。データは通常 `%LOCALAPPDATA%\Sub2 Personal` に保存されます。

詳細は [Personal Edition V1](docs/PERSONAL_EDITION_V1.md) を参照してください。
