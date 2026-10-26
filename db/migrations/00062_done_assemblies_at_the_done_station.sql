-- +goose Up
-- A done assembly closes at the station of its most recently done part, the line's done station,
-- where a finished single item lands (ADR 0012); before, it closed at the station that planned
-- it. This moves the done assemblies closed that way, each to its own parts' station within its
-- factory, in the order of LastDonePartStation (queries/parts.sql). Cancelled and open items stay
-- where they are. The span trigger (00059) fires on the station change but a done item has no
-- open span to close and opens none, so this writes no span; updated_at keeps the close time.
-- A second run changes no row.
WITH moved AS (
  SELECT a.id,
         (SELECT p.station FROM work_items p
           WHERE p.parent_id = a.id AND p.factory_id = a.factory_id AND p.part_key IS NOT NULL AND p.state = 'done'
           ORDER BY p.done_at DESC NULLS LAST, p.number DESC
           LIMIT 1) AS station
    FROM work_items a
   WHERE a.state = 'done'
)
UPDATE work_items w
   SET station = m.station
  FROM moved m
 WHERE w.id = m.id AND m.station IS NOT NULL AND w.station <> m.station;
