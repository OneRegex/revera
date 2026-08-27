/-
The two shipped Vego programs, embedded verbatim from the JSON artifacts that every target printer consumes.
The Lean theorems are about these exact bytes.
-/

import Vego.Decode

namespace Vego

def probeJsonText : String := include_str "../../go1/probe.vego.json"

def reveraJsonText : String := include_str "../../go1/revera.vego.json"

end Vego
