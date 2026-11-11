CREATE TABLE bp_wecom_settings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL DEFAULT 'WeCom bot',
    singleton boolean NOT NULL DEFAULT true UNIQUE CHECK (singleton),
    is_enabled boolean NOT NULL DEFAULT false,
    bot_id text NOT NULL DEFAULT '' CHECK (char_length(bot_id) <= 128),
    encrypted_bot_secret text,
    ws_url text NOT NULL DEFAULT 'wss://openws.work.weixin.qq.com' CHECK (ws_url ~ '^wss?://'),
    version integer NOT NULL DEFAULT 1,
    state text NOT NULL DEFAULT 'disabled' CHECK (state IN ('disabled', 'connecting', 'connected', 'failed')),
    state_at timestamptz,
    connected_at timestamptz,
    last_error text,
    CONSTRAINT bp_wecom_settings_ready CHECK (NOT is_enabled OR (bot_id <> '' AND encrypted_bot_secret IS NOT NULL))
);
INSERT INTO bp_wecom_settings(id, name, is_enabled, bot_id, encrypted_bot_secret, ws_url, version, state, state_at, connected_at, last_error)
SELECT id, name, is_enabled, app_id, encrypted_bot_secret, address, version, state, state_at, connected_at, last_error FROM bp_chat_bots WHERE platform = 'wecom';

CREATE TABLE bp_wecom_accounts (
    user_id uuid PRIMARY KEY REFERENCES auth_users(id) ON DELETE CASCADE,
    wecom_userid text NOT NULL UNIQUE CHECK (char_length(wecom_userid) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO bp_wecom_accounts(user_id, wecom_userid, created_at)
SELECT user_id, external_id, created_at FROM bp_chat_accounts WHERE platform = 'wecom';

CREATE TABLE bp_wecom_cards (
    task_id text PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('release', 'retry')),
    target_ip inet NOT NULL,
    job_id uuid REFERENCES bp_release_jobs(id) ON DELETE CASCADE,
    wecom_userid text NOT NULL,
    chat_type text NOT NULL CHECK (chat_type IN ('single', 'group')),
    chat_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    used_at timestamptz,
    CONSTRAINT bp_wecom_cards_retry_job CHECK ((kind = 'retry') = (job_id IS NOT NULL))
);
INSERT INTO bp_wecom_cards(task_id, kind, target_ip, job_id, wecom_userid, chat_type, chat_id, created_at, used_at)
SELECT task_id, kind, target_ip, job_id, owner_id, chat_type, chat_id, created_at, used_at FROM bp_chat_cards WHERE platform = 'wecom';

CREATE TABLE bp_wecom_jobs (
    job_id uuid PRIMARY KEY REFERENCES bp_release_jobs(id) ON DELETE CASCADE,
    wecom_userid text NOT NULL,
    chat_type text NOT NULL CHECK (chat_type IN ('single', 'group')),
    chat_id text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    notified_at timestamptz
);
INSERT INTO bp_wecom_jobs(job_id, wecom_userid, chat_type, chat_id, attempts, notified_at)
SELECT job_id, owner_id, chat_type, chat_id, attempts, notified_at FROM bp_chat_jobs WHERE platform = 'wecom';
CREATE INDEX bp_wecom_jobs_pending_idx ON bp_wecom_jobs(job_id) WHERE notified_at IS NULL;

DROP TABLE bp_chat_jobs, bp_chat_cards, bp_chat_accounts, bp_chat_bots;

-- Jobs started from Feishu are kept, as from the console: the audit trail outlives the bot.
UPDATE bp_release_jobs SET source = 'console' WHERE source = 'feishu';
ALTER TABLE bp_release_jobs DROP CONSTRAINT bp_release_jobs_source_check,
    ADD CONSTRAINT bp_release_jobs_source_check CHECK (source IN ('console', 'wecom'));
