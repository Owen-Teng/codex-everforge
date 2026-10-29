/* Popup-only candidate copy; source notices remain plain original text. */
(() => {const rows=[
 ['盲比','Duel','対戦','ดวล'],['消息','Messages','メッセージ','ข้อความ'],['编辑人物卡','Edit card','カードを編集','แก้ไขการ์ด'],['成长','Growth','成長','เติบโต'],['双卡盲比','Card clash','カード対戦','ปะทะการ์ด'],
 ['同周期示例投入，开牌后再揭晓。','Same-period sample effort. Flip to reveal.','同期間の模擬データ。カードを開いて結果を確認。','ค่าทุ่มเทตัวอย่างช่วงเดียวกัน พลิกการ์ดเพื่อเปิดผล'],
 ['你的卡片','Your card','自分のカード','การ์ดของคุณ'],['等待开牌','Ready to flip','開示を待機中','รอเปิดการ์ด'],
 ['胜者整卡展示给对手，作品高亮；不会自动公开。','The winner’s whole card and project are shown to the opponent; not automatically public.','勝者のカードと作品を相手に表示。自動公開はしません。','แสดงการ์ดผู้ชนะและผลงานให้คู่แข่ง ไม่เผยแพร่อัตโนมัติ'],
 ['开牌对撞','Flip and clash','開いて対戦','พลิกและปะทะ'],['双卡已锁定','Both cards locked','両カードを確定','ล็อกการ์ดทั้งสองแล้ว'],['开牌','Flipping','カード開示','กำลังพลิก'],['投入相遇','Efforts collide','取り組みが出会う','การทุ่มเทมาพบกัน'],['败方卡片碎裂','Losing card fractures','敗者カードが砕ける','การ์ดผู้แพ้แตก'],['本局已结算','Duel settled','対戦確定','สรุปรอบแล้ว'],
 ['跳过动画','Skip animation','演出をスキップ','ข้ามภาพเคลื่อนไหว'],['取消本局','Cancel duel','対戦を取消','ยกเลิกรอบนี้'],
 ['卡片保留开局版本，经验只结算一次。','Cards retain their starting version. XP settles only once.','カードは開始時のもの。経験値は一度だけ確定します。','การ์ดคงเวอร์ชันเริ่มต้น สรุป XP เพียงครั้งเดียว'],
 ['联调消息','Integration replay','連携テストのメッセージ','ข้อความทดสอบเชื่อมต่อ'],
 ['仅显示本机回放，不是正式X连接或系统通知中心。','Local replay only, not a live X connection or Windows notification center.','ローカル再生のみ。正式なX接続やWindows通知センターではありません。','เป็นการเล่นข้อมูลในเครื่อง ไม่ใช่ X สดหรือศูนย์แจ้งเตือน Windows'],
 ['合成回放','Synthetic replay','合成データ再生','ข้อมูลจำลองเล่นซ้ำ'],
 ['消息来源逐条标记；作者声明不代表本机额度已恢复。','Each message shows its source; an author claim does not confirm local quota recovery.','各メッセージに出所を表示。投稿者の主張は端末の利用枠回復を証明しません。','แต่ละข้อความแสดงแหล่งที่มา คำกล่าวของผู้โพสต์ไม่ยืนยันว่าโควตาในเครื่องฟื้นแล้ว'],
 ['X 来源 · 作者声明，未核实本机恢复','X source · author claim, local recovery unverified','X由来 · 投稿者の主張、端末の回復は未確認','แหล่ง X · คำกล่าวของผู้โพสต์ ยังไม่ยืนยันการฟื้นในเครื่อง'],
 ['等待新消息。宿主启动前已有内容不会弹出。','Waiting for new messages. Existing content is silent at startup.','新しいメッセージを待機中。起動前の内容は通知しません。','รอข้อความใหม่ เนื้อหาก่อนเริ่มจะไม่แจ้งเตือน'],
 ['消息提示','Message prompts','メッセージ通知','การแจ้งข้อความ'],
 ['已允许读取 X 来源文件；不代表连接已成功。','X source files are allowed; this does not confirm a successful connection.','X由来ファイルの読取を許可。接続成功を意味しません。','อนุญาตให้อ่านไฟล์จาก X ไม่ได้ยืนยันว่าเชื่อมต่อสำเร็จ'],
 ['仅接收合成联调消息；正式来源未启用。','Synthetic test messages only; live sources are not enabled.','合成テストメッセージのみ。正式な接続は未有効です。','รับเฉพาะข้อความทดสอบจำลอง ยังไม่เปิดแหล่งจริง'],
 ['等待新的联调消息。宿主启动前已有内容不会弹出。','Waiting for a new replay notice. Existing content is silent at startup.','新しいテストメッセージを待機中。起動前の内容は通知しません。','รอข้อความทดสอบใหม่ เนื้อหาที่มีอยู่ก่อนเริ่มจะไม่แจ้งเตือน'],
 ['所有卡片与经验仍为独立模拟。','Cards and XP remain independently simulated.','カードと経験値は独立した模擬データです。','การ์ดและ XP ยังเป็นข้อมูลจำลองแยก'],
 ['跳过开牌、碰撞与碎裂，直接显示结算。','Skip flip, clash and fracture; show the result directly.','開示・衝突・破砕を省略し、結果を表示。','ข้ามพลิก ปะทะ และแตก แสดงผลโดยตรง'],
 ['不连接真实账号、不请求密钥。','No real account connection or key request.','実アカウントや秘密鍵は使用しません。','ไม่เชื่อมบัญชีจริงหรือขอคีย์'],
 ['联调提示','Replay prompt','連携テスト通知','การแจ้งทดสอบ'],
 ['本机联调消息，正式X来源待配置。','Local integration messages. Live X source is not configured.','ローカルのテストメッセージ。正式なX接続は未設定です。','ข้อความทดสอบในเครื่อง ยังไม่ได้ตั้งค่าแหล่ง X จริง'],
 ['查看消息','View messages','メッセージを見る','ดูข้อความ'],['桌面候选 · 模拟社区','Desktop candidate · simulated community','デスクトップ候補 · 模擬コミュニティ','ตัวอย่างเดสก์ท็อป · ชุมชนจำลอง']
 ];for(const [source,en,ja,th]of rows)for(const[language,value]of Object.entries({en,ja,th}))(window.COMMUNITY_TRANSLATIONS[language]||={})[source]=value;})();

