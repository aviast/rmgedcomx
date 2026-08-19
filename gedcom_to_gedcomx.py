import sys
import re
import requests

def parse_gedcom(filename):
    """Parses a GEDCOM file into a dictionary of individuals and families."""
    records = {'INDI': {}, 'FAM': {}}
    current_id, current_type, current_tag, current_famc_id = None, None, None, None

    with open(filename, 'r', encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if not line: continue

            parts = line.split(' ', 2)
            level = int(parts[0])
            tag = parts[1]
            value = parts[2] if len(parts) > 2 else ""

            if level == 0:
                if value in ['INDI', 'FAM']:
                    current_id = tag
                    current_type = value
                    records[current_type][current_id] = {
                        'id': current_id,
                        'names': [],
                        'events': {},
                        'famc': {}
                    }
                else:
                    current_id = None
                    current_type = None

            elif level == 1 and current_id:
                current_tag = tag
                if tag == 'SEX':
                    records[current_type][current_id][tag] = value
                elif tag in ['NAME', 'ALIA']:
                    is_preferred = (tag == 'NAME')
                    records[current_type][current_id]['names'].append({
                        'value': value,
                        'parts': {},
                        'nicknames': [],
                        'preferred': is_preferred
                    })
                elif tag in ['HUSB', 'WIFE', 'CHIL']:
                    records[current_type][current_id].setdefault(tag, []).append(value)
                elif tag == 'FAMC' and current_type == 'INDI':
                    current_famc_id = value
                    records[current_type][current_id]['famc'][current_famc_id] = {}
                elif tag in ['BIRT', 'CHR', 'DEAT', 'BURI', 'MARR', 'DIV', 'ADOP', 'ANUL', 'ENGA', 'SEPR', 'OCCU', 'EDUC', 'RELI', 'TITL']:
                    records[current_type][current_id]['events'][tag] = {}
                    # Some attributes (like OCCU, EDUC) include the value directly on the level-1 line
                    if value:
                        records[current_type][current_id]['events'][tag]['value'] = value

            elif level == 2 and current_id and current_tag:
                # Level 2 Name Parts (GIVN, SURN) and Nicknames (NICK) for both NAME and ALIA tags
                if current_tag in ['NAME', 'ALIA']:
                    if tag in ['GIVN', 'SURN']:
                        if records[current_type][current_id]['names']:
                            records[current_type][current_id]['names'][-1]['parts'][tag] = value
                    elif tag == 'NICK':
                        if records[current_type][current_id]['names']:
                            records[current_type][current_id]['names'][-1]['nicknames'].append(value)
                # Level 2 Pedigree under FAMC (PEDI adopted / birth / foster / step)
                elif current_tag == 'FAMC' and current_type == 'INDI' and tag == 'PEDI':
                    if current_famc_id and current_famc_id in records[current_type][current_id]['famc']:
                        records[current_type][current_id]['famc'][current_famc_id]['pedi'] = value
                # Level 2 Event details (DATE, PLAC, FAMC pointer for ADOP)
                elif current_tag in ['BIRT', 'CHR', 'DEAT', 'BURI', 'MARR', 'DIV', 'ADOP', 'ANUL', 'ENGA', 'SEPR', 'OCCU', 'EDUC', 'RELI', 'TITL'] and tag in ['DATE', 'PLAC', 'FAMC']:
                    records[current_type][current_id]['events'][current_tag][tag] = value

    return records

def normalize_gedcom_date(date_str):
    """Converts a GEDCOM date string into a GEDCOM X formal date string (+YYYY-MM-DD)."""
    if not date_str:
        return None

    months = {
        'JAN': '01', 'FEB': '02', 'MAR': '03', 'APR': '04',
        'MAY': '05', 'JUN': '06', 'JUL': '07', 'AUG': '08',
        'SEP': '09', 'OCT': '10', 'NOV': '11', 'DEC': '12'
    }

    parts = date_str.strip().upper().split()
    if not parts:
        return None

    prefix = ""
    suffix = ""
    is_approximate = False

    if parts[0] in ['ABT', 'CAL', 'EST']:
        is_approximate = True
        parts = parts[1:]
    elif parts[0] == 'BEF':
        prefix = "/"
        parts = parts[1:]
    elif parts[0] == 'AFT':
        suffix = "/"
        parts = parts[1:]

    year, month, day = None, None, None
    for part in reversed(parts):
        if part.isdigit():
            if not year:
                # First numeric token from right is year; pad to 4 digits per GEDCOM X spec
                year = part.zfill(4)
            else:
                day = part.zfill(2)
        elif part in months:
            month = months[part]

    if not year:
        return None

    formal_date = f"+{year}"
    if month:
        formal_date += f"-{month}"
        if day:
            formal_date += f"-{day}"

    if is_approximate:
        formal_date = f"A{formal_date}"

    return f"{prefix}{formal_date}{suffix}"

def build_person_doc(ind_id, ind_data):
    """Builds a GEDCOM X document for a single person."""
    person = {"names": [], "facts": []}

    preferred_names = []
    alternate_names = []

    # Process all parsed names and aliases
    for name_data in ind_data.get('names', []):
        raw_name = name_data['value']
        name_str = raw_name.replace('/', '').strip()
        name_form = {"fullText": name_str}

        parts = []
        if name_data['parts']:
            if 'GIVN' in name_data['parts']:
                parts.append({"type": "http://gedcomx.org/Given", "value": name_data['parts']['GIVN']})
            if 'SURN' in name_data['parts']:
                parts.append({"type": "http://gedcomx.org/Surname", "value": name_data['parts']['SURN']})
        elif '/' in raw_name:
            match = re.search(r'^(.*?)/(.*?)/(.*?)$', raw_name)
            if match:
                given1, surname, given2 = match.groups()
                given = (given1.strip() + " " + given2.strip()).strip()
                surname = surname.strip()
                if given:
                    parts.append({"type": "http://gedcomx.org/Given", "value": given})
                if surname:
                    parts.append({"type": "http://gedcomx.org/Surname", "value": surname})

        if parts:
            name_form["parts"] = parts

        gx_name = {"nameForms": [name_form]}

        # Determine sorting preference
        if name_data['preferred']:
            gx_name["preferred"] = True
            preferred_names.append(gx_name)
        else:
            gx_name["preferred"] = False
            alternate_names.append(gx_name)

        # Process Nicknames into their own distinct name entities
        for nick in name_data.get('nicknames', []):
            nick_name_obj = {
                "type": "http://gedcomx.org/Nickname",
                "nameForms": [{"fullText": nick}],
                "preferred": False
            }
            alternate_names.append(nick_name_obj)

    # GEDCOM X specs indicate the preferred name comes first in the array
    person['names'] = preferred_names + alternate_names

    if 'SEX' in ind_data:
        gender_type = "http://gedcomx.org/Male" if ind_data['SEX'] == 'M' else "http://gedcomx.org/Female"
        person['gender'] = {"type": gender_type}

    event_mapping = {
        'BIRT': 'http://gedcomx.org/Birth',
        'DEAT': 'http://gedcomx.org/Death',
        'CHR':  'http://gedcomx.org/Christening',
        'BURI': 'http://gedcomx.org/Burial',
        'ADOP': 'http://gedcomx.org/Adoption',
        'OCCU': 'http://gedcomx.org/Occupation',
        'EDUC': 'http://gedcomx.org/Education',
        'RELI': 'http://gedcomx.org/Religion',
        'TITL': 'http://gedcomx.org/TitleOfNobility'
    }

    for event_tag, event_data in ind_data.get('events', {}).items():
        if event_tag in event_mapping:
            fact = {"type": event_mapping[event_tag]}

            # Embed values for attributes like OCCU, EDUC, RELI, TITL
            if 'value' in event_data:
                fact["value"] = event_data['value']

            if 'DATE' in event_data:
                fact["date"] = {"original": event_data['DATE']}
                formal_date = normalize_gedcom_date(event_data['DATE'])
                if formal_date:
                    fact["date"]["formal"] = formal_date
            if 'PLAC' in event_data:
                fact["place"] = {"original": event_data['PLAC']}
            person['facts'].append(fact)

    return {"persons": [person]}

def build_relationship_doc(rel_type, person1_uri, person2_uri, facts=None):
    """Builds a GEDCOM X document for a single relationship."""
    rel = {
        "type": rel_type,
        "person1": {"resource": person1_uri},
        "person2": {"resource": person2_uri}
    }
    if facts:
        rel["facts"] = facts
    return {"relationships": [rel]}

def discover_endpoints(server_url, headers):
    """GETs the root collection to discover hypermedia links."""
    r = requests.get(server_url, headers=headers)
    r.raise_for_status()

    data = r.json()
    collections = data.get('collections', [])
    if not collections:
        raise ValueError("No collections found at the root URL.")

    first_collection = collections[0]
    links = first_collection.get('links', {})

    persons_url = links.get('persons', {}).get('href')
    rels_url = links.get('relationships', {}).get('href')

    if not persons_url or not rels_url:
        raise ValueError("Could not discover 'persons' or 'relationships' endpoints.")

    return persons_url, rels_url

def upload_data(server_url, gedcom_data):
    """Executes state machine upload for persons, couples (including divorces), and parent-child facts."""
    headers = {
        "Content-Type": "application/x-gedcomx-v1+json",
        "Accept": "application/x-gedcomx-v1+json"
    }

    try:
        print(f"Discovering endpoints at {server_url} ...")
        persons_url, rels_url = discover_endpoints(server_url, headers)

        id_map = {}

        # 1. Upload Persons
        print(f"Uploading individuals to {persons_url} ...")
        for ind_id, ind_data in gedcom_data['INDI'].items():
            doc = build_person_doc(ind_id, ind_data)
            r = requests.post(persons_url, json=doc, headers=headers)
            r.raise_for_status()

            server_uri = r.headers.get('Location')
            if not server_uri:
                raise ValueError(f"Server did not return a Location header for person {ind_id}")
            id_map[ind_id] = server_uri

        # 2. Upload Relationships
        print(f"Uploading relationships to {rels_url} ...")

        # Fact mappings for couple relationships
        couple_event_map = {
            'MARR': 'http://gedcomx.org/Marriage',
            'DIV':  'http://gedcomx.org/Divorce',
            'ANUL': 'http://gedcomx.org/Annulment',
            'ENGA': 'http://gedcomx.org/Engagement',
            'SEPR': 'http://gedcomx.org/Separation'
        }

        # Fact mappings for parent-child relationship pedigree
        pedi_map = {
            'ADOPTED':  'http://gedcomx.org/AdoptiveParent',
            'BIRTH':    'http://gedcomx.org/BiologicalParent',
            'FOSTER':   'http://gedcomx.org/FosterParent',
            'STEP':     'http://gedcomx.org/StepParent',
            'GUARDIAN': 'http://gedcomx.org/GuardianParent'
        }

        for fam_id, fam_data in gedcom_data['FAM'].items():
            husbands = fam_data.get('HUSB', [])
            wives = fam_data.get('WIFE', [])
            children = fam_data.get('CHIL', [])

            # --- Couples & Couple Facts (Marriage, Divorce, etc.) ---
            for husb in husbands:
                for wife in wives:
                    if husb in id_map and wife in id_map:
                        couple_facts = []

                        for ev_tag, fact_uri in couple_event_map.items():
                            if ev_tag in fam_data.get('events', {}):
                                ev_info = fam_data['events'][ev_tag]
                                fact = {"type": fact_uri}
                                if 'DATE' in ev_info:
                                    fact["date"] = {"original": ev_info['DATE']}
                                    formal_date = normalize_gedcom_date(ev_info['DATE'])
                                    if formal_date:
                                        fact["date"]["formal"] = formal_date
                                if 'PLAC' in ev_info:
                                    fact["place"] = {"original": ev_info['PLAC']}
                                couple_facts.append(fact)

                        doc = build_relationship_doc("http://gedcomx.org/Couple", id_map[husb], id_map[wife], couple_facts)
                        r = requests.post(rels_url, json=doc, headers=headers)
                        r.raise_for_status()

            # --- Parent-Child & Pedigree Facts (Adoptive, Biological, etc.) ---
            parents = husbands + wives
            for parent in parents:
                for child in children:
                    if parent in id_map and child in id_map:
                        pc_facts = []

                        # Inspect child record for PEDI tag or ADOP event pointing to this family
                        child_famc_info = gedcom_data['INDI'].get(child, {}).get('famc', {}).get(fam_id, {})
                        pedi_val = child_famc_info.get('pedi', '').upper()

                        child_events = gedcom_data['INDI'].get(child, {}).get('events', {})
                        has_adop_event = 'ADOP' in child_events and (
                            child_events['ADOP'].get('FAMC') == fam_id or not child_events['ADOP'].get('FAMC')
                        )

                        if pedi_val in pedi_map:
                            pc_facts.append({"type": pedi_map[pedi_val]})
                        elif has_adop_event:
                            pc_facts.append({"type": "http://gedcomx.org/AdoptiveParent"})

                        doc = build_relationship_doc("http://gedcomx.org/ParentChild", id_map[parent], id_map[child], facts=pc_facts)
                        r = requests.post(rels_url, json=doc, headers=headers)
                        r.raise_for_status()

        print("Upload completed successfully!")

    except requests.exceptions.RequestException as e:
        print(f"HTTP Error: {e}")
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    if len(sys.argv) not in [2, 3]:
        print("Usage: python gedcom_to_gedcomx.py <gedcom_filename> [gedcomx_server_url]")
        sys.exit(1)

    filename = sys.argv[1]
    server_url = sys.argv[2] if len(sys.argv) == 3 else "http://localhost:8080/"

    print(f"GEDCOM Filename : {filename}")
    print(f"GEDCOM X Server : {server_url}")
    print(f"Parsing {filename}...")

    parsed_data = parse_gedcom(filename)
    upload_data(server_url, parsed_data)
