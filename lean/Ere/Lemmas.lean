/-
Universal lemmas about the model, proved by ordinary reasoning rather than evaluation.

The computable closure `icaseClosure` enumerates case preimages, while section 10.2 speaks of replacing
characters of an accepted subject by their counterparts. In the POSIX locale the two agree for every
acceptance predicate and every character, which is what `posix_icaseClosure_iff` says.
-/

import Ere.Semantics

namespace Ere

namespace Posix

theorem casePreimages_sound (c d : Chr) (h : d ∈ casePreimages c) :
    toUpper d = c ∨ toLower d = c := by
  unfold casePreimages at h
  split at h
  · rename_i hu
    simp only [List.mem_singleton] at h
    subst h
    left
    unfold toUpper lower
    simp only [upper, Bool.and_eq_true, decide_eq_true_eq] at hu
    rw [if_pos (by simp only [Bool.and_eq_true, decide_eq_true_eq]; omega)]
    omega
  · split at h
    · rename_i hu hl
      simp only [List.mem_singleton] at h
      subst h
      right
      unfold toLower upper
      simp only [lower, Bool.and_eq_true, decide_eq_true_eq] at hl
      rw [if_pos (by simp only [Bool.and_eq_true, decide_eq_true_eq]; omega)]
      omega
    · simp at h

end Posix

/-- Without `REG_ICASE` the closure is the acceptance predicate itself. -/
theorem icaseClosure_false (loc : Locale) (S : Chr → Bool) (t : Chr) :
    icaseClosure loc false S t = S t := by
  simp [icaseClosure]

/-- In the POSIX locale, the computable closure is exactly the closure rule of section 10.2. -/
theorem posix_icaseClosure_iff (S : Chr → Bool) (t : Chr) :
    icaseClosure posixLocale true S t = true ↔ ClosureProp posixLocale S t := by
  unfold icaseClosure ClosureProp
  simp only [Bool.or_eq_true, Bool.true_and, List.any_eq_true]
  constructor
  · rintro (h | ⟨p, hp, hS⟩)
    · exact ⟨t, h, Or.inl rfl⟩
    · refine ⟨p, hS, ?_⟩
      rcases Posix.casePreimages_sound t p hp with h | h
      · exact Or.inr (Or.inl h.symm)
      · exact Or.inr (Or.inr h.symm)
  · rintro ⟨c', hS, h | h | h⟩
    · subst h; exact Or.inl hS
    · rcases Posix.casePreimages_complete t c' (Or.inl h.symm) with h' | h'
      · subst h'; exact Or.inl hS
      · exact Or.inr ⟨c', h', hS⟩
    · rcases Posix.casePreimages_complete t c' (Or.inr h.symm) with h' | h'
      · subst h'; exact Or.inl hS
      · exact Or.inr ⟨c', h', hS⟩

end Ere
