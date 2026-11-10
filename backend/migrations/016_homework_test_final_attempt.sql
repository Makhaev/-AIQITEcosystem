-- The first submitted attempt is final, including incorrect answers.
CREATE FUNCTION guard_final_homework_test_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.attempts>0 AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'Submitted test answers are final' USING ERRCODE='23514',CONSTRAINT='homework_test_attempt_final';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER homework_test_attempt_final BEFORE UPDATE ON homework_test_assignments
FOR EACH ROW EXECUTE FUNCTION guard_final_homework_test_attempt();
