-- A command profile speaks one vendor's commands, and a vendor can have several profiles. A switch
-- may only use a profile of its own vendor.
ALTER TABLE bp_command_profiles
    ADD COLUMN vendor text NOT NULL DEFAULT 'other' CHECK (vendor IN ('huawei', 'h3c', 'ruijie', 'cisco', 'other'));

-- Existing profiles take the vendor most of their switches have; unused ones stay "other".
UPDATE bp_command_profiles p SET vendor = v.vendor
FROM (SELECT DISTINCT ON (command_profile_id) command_profile_id, vendor
      FROM bp_switches GROUP BY command_profile_id, vendor
      ORDER BY command_profile_id, count(*) DESC, vendor) v
WHERE v.command_profile_id = p.id;

ALTER TABLE bp_command_profiles ADD CONSTRAINT bp_command_profiles_id_vendor_key UNIQUE (id, vendor);
ALTER TABLE bp_switches ADD CONSTRAINT bp_switches_profile_vendor_fkey
    FOREIGN KEY (command_profile_id, vendor) REFERENCES bp_command_profiles (id, vendor) NOT VALID;

-- Switches that already disagree with their profile keep working until their vendor or profile
-- changes. With none, the constraint holds for every row.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM bp_switches s JOIN bp_command_profiles p ON p.id = s.command_profile_id WHERE s.vendor <> p.vendor) THEN
        ALTER TABLE bp_switches VALIDATE CONSTRAINT bp_switches_profile_vendor_fkey;
    END IF;
END $$;
