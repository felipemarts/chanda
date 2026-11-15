-- 0137_event_role: the speaker role of a session turn (decision 0137).
--
-- log_session wrote user, assistant and system turns from the same session
-- source as events of kind turn, so who spoke was lost. role keeps the
-- turn's role as the call gave it (user, assistant, system, tool); it is
-- NULL for every other event and for turns written before this migration.
-- Like every other column of the raw log it never changes after insert.
ALTER TABLE events ADD COLUMN role TEXT;

DROP TRIGGER events_immutable;
CREATE TRIGGER events_immutable BEFORE UPDATE ON events
WHEN NEW.id IS NOT OLD.id OR NEW.namespace IS NOT OLD.namespace OR NEW.source_id IS NOT OLD.source_id
  OR NEW.kind IS NOT OLD.kind OR NEW.content IS NOT OLD.content OR NEW.content_hash IS NOT OLD.content_hash
  OR NEW.trust_zone IS NOT OLD.trust_zone OR NEW.actor IS NOT OLD.actor OR NEW.recorded_at IS NOT OLD.recorded_at
  OR NEW.valid_from IS NOT OLD.valid_from OR NEW.doc_id IS NOT OLD.doc_id OR NEW.doc_version IS NOT OLD.doc_version
  OR NEW.ordinal IS NOT OLD.ordinal OR NEW.heading_path IS NOT OLD.heading_path OR NEW.session_id IS NOT OLD.session_id
  OR NEW.flags IS NOT OLD.flags OR NEW.role IS NOT OLD.role
BEGIN
  SELECT RAISE(ABORT, 'immutable: events are append-only; only retired_at may change');
END;
