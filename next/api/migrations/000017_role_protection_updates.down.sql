-- Restores the 000013 definition, including its no-op role updates.
CREATE OR REPLACE FUNCTION bp_protect_roles() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.name_canonical IN ('operator', 'admin') THEN
        RAISE EXCEPTION 'bind-plane business roles cannot be changed';
    END IF;
    RETURN OLD;
END $$;
