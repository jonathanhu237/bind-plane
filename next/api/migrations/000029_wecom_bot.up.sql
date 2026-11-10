-- Releasing through a WeCom (企业微信) bot. The API keeps one long connection to WeCom with the bot's
-- ID and secret, set by admins on the WeCom page; nothing about it lives in the environment.

-- Where a release was started: the release console, or the WeCom bot.
ALTER TABLE bp_release_jobs
    ADD COLUMN source text NOT NULL DEFAULT 'console' CHECK (source IN ('console', 'wecom'));

-- One row. The connector writes the state columns; editing the settings bumps the version, which
-- makes it reconnect with them.
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
INSERT INTO bp_wecom_settings DEFAULT VALUES;

-- Which WeCom member is which Bind Plane user. The user's own roles decide what they may do.
CREATE TABLE bp_wecom_accounts (
    user_id uuid PRIMARY KEY REFERENCES auth_users(id) ON DELETE CASCADE,
    wecom_userid text NOT NULL UNIQUE CHECK (char_length(wecom_userid) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- A card the bot sent with buttons: what a click on it releases, and in which chat. One click only.
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

-- A release started in WeCom, and the chat its result goes back to once it finishes.
CREATE TABLE bp_wecom_jobs (
    job_id uuid PRIMARY KEY REFERENCES bp_release_jobs(id) ON DELETE CASCADE,
    wecom_userid text NOT NULL,
    chat_type text NOT NULL CHECK (chat_type IN ('single', 'group')),
    chat_id text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    notified_at timestamptz
);
CREATE INDEX bp_wecom_jobs_pending_idx ON bp_wecom_jobs(job_id) WHERE notified_at IS NULL;
