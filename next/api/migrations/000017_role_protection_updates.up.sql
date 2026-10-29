-- 000013 returned OLD from a BEFORE UPDATE trigger, which silently turned every
-- role update into a no-op. An update must return NEW; a delete still returns OLD.
CREATE OR REPLACE FUNCTION bp_protect_roles() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.name_canonical IN ('operator', 'admin') THEN
        RAISE EXCEPTION 'bind-plane business roles cannot be changed';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END $$;
