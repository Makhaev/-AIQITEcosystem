package curators

// Signals are factual reasons to review a pupil, not a predicted risk score.
// The same predicates drive the overview and the filtered student list.
const missingFamily = `NOT EXISTS(SELECT 1 FROM student_parents sp JOIN parents p ON p.id=sp.parent_id WHERE sp.student_id=s.id AND p.status='active')`
const recentAbsences = `(SELECT count(*) FROM attendance att JOIN lessons l ON l.id=att.lesson_id WHERE att.student_id=s.id AND att.included AND att.status='absent' AND l.status='completed' AND l.starts_at>=now()-interval '30 days' AND l.starts_at<=now())`
const overdueWork = `(SELECT count(*) FROM assignments work JOIN lessons l ON l.id=work.lesson_id WHERE work.student_id=s.id AND work.status='assigned' AND work.due_at<now() AND l.status<>'cancelled')`
const attentionCondition = `(` + missingFamily + ` OR ` + recentAbsences + `>0 OR ` + overdueWork + `>0)`
const attentionJSON = `jsonb_build_object('missing_family',` + missingFamily + `,'absences',` + recentAbsences + `,'overdue',` + overdueWork + `)`
