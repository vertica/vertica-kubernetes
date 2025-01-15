# util/

The util directory contains code that
1. Provides useful functionality that could be used by more than one library in this module
2. Doesn't fall into an existing specialized category, like logging
3. Has a relatively limited scope. "Limited scope" is a subjective criteria. If there are many
   objects and functions that all interact together, consider placing the code in its own
   top-level directory and not here.