// Letters and digits that are hard to confuse when a password is read out or typed from a note.
const upper = 'ABCDEFGHJKLMNPQRSTUVWXYZ'
const lower = 'abcdefghijkmnopqrstuvwxyz'
const digits = '23456789'
const special = '!#$%&*+-=?@^_'
const all = upper + lower + digits + special

function pick(characters: string, random: Uint32Array, index: number): string {
  return characters[random[index] % characters.length]
}

/** A random password that follows the rules for new passwords: one of each kind of character, then any. */
export function generatePassword(length = 16): string {
  const random = new Uint32Array(length * 2)
  crypto.getRandomValues(random)
  const characters = [pick(upper, random, 0), pick(lower, random, 1), pick(digits, random, 2), pick(special, random, 3)]
  for (let index = characters.length; index < length; index += 1) characters.push(pick(all, random, index))
  // Shuffle so the required kinds are not always first.
  for (let index = characters.length - 1; index > 0; index -= 1) {
    const other = random[length + index] % (index + 1)
    ;[characters[index], characters[other]] = [characters[other], characters[index]]
  }
  return characters.join('')
}
