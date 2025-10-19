# Code Conventions

## Design Philosophy (Linus-Style)

1. **Good Taste - Eliminate Special Cases**

   - Use interfaces over if/else chains
   - Design data structures to make edge cases disappear
   - Prefer polymorphism to conditionals

2. **Shallow Indentation**

   - Functions should not exceed 3 levels of indentation
   - Early returns over nested conditionals
   - Extract complex logic into helper functions

3. **Clear Naming**

   - Use domain-specific names: `Provider`, `Executor`, `Handler`
   - Avoid generic names: `Manager`, `Service`, `Helper`
   - Package names match their primary type

4. **Error Visibility**

   - Don't hide errors in logs
   - Surface errors to users (GitHub comments)
   - Include context in error messages

5. **Backward Compatibility**
   - Provider interface designed for future extension
   - Config fields have sensible defaults
   - No breaking API changes without major version bump
