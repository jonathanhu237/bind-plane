-- Releasing from Feishu (飞书) as well as WeCom. Both bots share one core, so their settings,
-- member bindings, cards and the chats results go back to share tables, told apart by platform.

ALTER TABLE bp_release_jobs DROP CONSTRAINT bp_release_jobs_source_check,
    ADD CONSTRAINT bp_release_jobs_source_check CHECK (source IN ('console', 'wecom', 'feishu'));

-- One row per platform. The connector writes the state columns; editing the settings bumps the
-- version, which makes it reconnect with them. app_id is WeCom's bot ID or Feishu's App ID, and
-- address is WeCom's long-connection address or Feishu's open platform.
CREATE TABLE bp_chat_bots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    platform text NOT NULL UNIQUE CHECK (platform IN ('wecom', 'feishu')),
    name text NOT NULL,
    is_enabled boolean NOT NULL DEFAULT false,
    app_id text NOT NULL DEFAULT '' CHECK (char_length(app_id) <= 128),
    encrypted_bot_secret text,
    address text NOT NULL CHECK (address ~ '^(wss?|https?)://'),
    version integer NOT NULL DEFAULT 1,
    state text NOT NULL DEFAULT 'disabled' CHECK (state IN ('disabled', 'connecting', 'connected', 'failed')),
    state_at timestamptz,
    connected_at timestamptz,
    last_error text,
    CONSTRAINT bp_chat_bots_ready CHECK (NOT is_enabled OR (app_id <> '' AND encrypted_bot_secret IS NOT NULL))
);
INSERT INTO bp_chat_bots(id, platform, name, is_enabled, app_id, encrypted_bot_secret, address, version, state, state_at, connected_at, last_error)
SELECT id, 'wecom', name, is_enabled, bot_id, encrypted_bot_secret, ws_url, version, state, state_at, connected_at, last_error FROM bp_wecom_settings;
INSERT INTO bp_chat_bots(platform, name, address) VALUES ('feishu', 'Feishu bot', 'https://open.feishu.cn');

-- Which member of a platform is which Bind Plane user. The user's own roles decide what they may do.
CREATE TABLE bp_chat_accounts (
    platform text NOT NULL CHECK (platform IN ('wecom', 'feishu')),
    user_id uuid NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, user_id),
    CONSTRAINT bp_chat_accounts_external_key UNIQUE (platform, external_id)
);
INSERT INTO bp_chat_accounts(platform, user_id, external_id, created_at)
SELECT 'wecom', user_id, wecom_userid, created_at FROM bp_wecom_accounts;

-- A card the bot sent with buttons: what a click on it releases, and in which chat. One click only.
CREATE TABLE bp_chat_cards (
    task_id text PRIMARY KEY,
    platform text NOT NULL CHECK (platform IN ('wecom', 'feishu')),
    kind text NOT NULL CHECK (kind IN ('release', 'retry')),
    target_ip inet NOT NULL,
    job_id uuid REFERENCES bp_release_jobs(id) ON DELETE CASCADE,
    owner_id text NOT NULL,
    chat_type text NOT NULL CHECK (chat_type IN ('single', 'group')),
    chat_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    used_at timestamptz,
    CONSTRAINT bp_chat_cards_retry_job CHECK ((kind = 'retry') = (job_id IS NOT NULL))
);
INSERT INTO bp_chat_cards(task_id, platform, kind, target_ip, job_id, owner_id, chat_type, chat_id, created_at, used_at)
SELECT task_id, 'wecom', kind, target_ip, job_id, wecom_userid, chat_type, chat_id, created_at, used_at FROM bp_wecom_cards;

-- A job started from a chat, and the chat its result goes back to once it finishes.
CREATE TABLE bp_chat_jobs (
    job_id uuid PRIMARY KEY REFERENCES bp_release_jobs(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK (platform IN ('wecom', 'feishu')),
    owner_id text NOT NULL,
    chat_type text NOT NULL CHECK (chat_type IN ('single', 'group')),
    chat_id text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    notified_at timestamptz
);
INSERT INTO bp_chat_jobs(job_id, platform, owner_id, chat_type, chat_id, attempts, notified_at)
SELECT job_id, 'wecom', wecom_userid, chat_type, chat_id, attempts, notified_at FROM bp_wecom_jobs;
CREATE INDEX bp_chat_jobs_pending_idx ON bp_chat_jobs(platform, job_id) WHERE notified_at IS NULL;

DROP TABLE bp_wecom_jobs, bp_wecom_cards, bp_wecom_accounts, bp_wecom_settings;
